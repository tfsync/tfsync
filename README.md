# tfsync

GitOps-native Terraform operator for Kubernetes. ArgoCD, but for Terraform.

tfsync watches a Git repository containing Terraform files and reconciles
them against real infrastructure. You declare a `Workspace` CRD pointing at a
repo+path; the controller runs `terraform init/plan/apply` inside isolated
Kubernetes Jobs and writes results back to the resource's status.

## Why

- **GitOps all the way down.** Infrastructure follows the same pull-based
  reconcile pattern as the rest of your cluster.
- **No long-running agents.** Each reconcile spawns a fresh, ephemeral runner
  Job — no shared state, no lingering processes.
- **Drift detection.** The operator re-plans on the sync interval and flips a
  workspace to `OutOfSync` when state drifts from Git.
- **Cloud-native credentials.** Provider and backend secrets are plain
  Kubernetes Secrets, projected into the runner Job — never written to status
  or logs.

## Architecture

```
  ┌───────────────┐    reconcile     ┌─────────────────┐
  │ Workspace CR  ├─────────────────▶│  Controller     │
  └───────────────┘                  │ (this operator) │
         ▲                           └────────┬────────┘
         │ status                             │ clone + build
         │                                    ▼
         │                        ┌─────────────────────┐
         │                        │ ConfigMap (.tf)     │
         │                        └──────────┬──────────┘
         │                                   │ mount
         │                                   ▼
         │                        ┌─────────────────────┐
         └── logs / exit status ──│ Runner Job          │
                                  │ hashicorp/terraform │
                                  └─────────────────────┘
```

## Quickstart

### Install the operator

```sh
# via raw manifests
make install          # installs the Workspace CRD
make deploy           # creates namespace, RBAC, and controller Deployment

# or via Helm
helm install tfsync ./charts/tfsync -n tfsync-system --create-namespace
```

### Create credentials and backend secrets

```sh
kubectl create secret generic aws-credentials \
  --from-literal=AWS_ACCESS_KEY_ID=... \
  --from-literal=AWS_SECRET_ACCESS_KEY=...

kubectl create secret generic tfsync-s3-backend \
  --from-literal=TF_BACKEND_BUCKET=my-tfstate \
  --from-literal=TF_BACKEND_KEY=envs/dev/terraform.tfstate \
  --from-literal=TF_BACKEND_REGION=us-east-1
```

### Define a Workspace

```yaml
apiVersion: tfsync.io/v1alpha1
kind: Workspace
metadata:
  name: example-aws-vpc
spec:
  source:
    repo: https://github.com/example/infra.git
    path: environments/dev/vpc
    branch: main
  syncPolicy:
    autoApply: false      # stop at OutOfSync; require manual approval
    interval: 10m
  backend:
    type: s3
    secretRef: tfsync-s3-backend
  credentials:
    secretRef: aws-credentials
```

```sh
kubectl apply -f workspace.yaml
```

## Using `tfsync`

```sh
make build-cli

./bin/tfsync list -n default
./bin/tfsync plan example-aws-vpc -n default
./bin/tfsync sync example-aws-vpc -n default   # triggers immediate reconcile
```

## Workspace lifecycle

| Phase         | Meaning                                                    |
|---------------|------------------------------------------------------------|
| Pending       | Reconcile queued, no work started yet                      |
| Initializing  | Git clone in progress                                      |
| Planning      | Runner Job executing `terraform plan`                      |
| Applying      | Runner Job executing `terraform apply` (autoApply=true)    |
| OutOfSync     | Plan shows drift; waiting for manual approval              |
| Synced        | Infrastructure matches Git; next check at `syncPolicy.interval` |
| Failed        | Clone or runner Job failed; see `.status.lastPlanOutput`   |

## ArgoCD integration (planned)

tfsync is designed to compose with ArgoCD rather than replace it. Because
`Workspace` is a regular Kubernetes CRD, ArgoCD can sync it like any other
manifest — the full GitOps story looks like:

```
┌──────────────────┐    syncs CRs    ┌────────────────────┐   reconciles    ┌──────────────┐
│ Git (Workspace   │ ──────────────▶ │ Kubernetes cluster │ ──────────────▶ │ Cloud infra  │
│ YAMLs)           │    (ArgoCD)     │ (Workspace CRs)    │    (tfsync)     │              │
└──────────────────┘                 └────────────────────┘                 └──────────────┘
```

A future release will ship an ArgoCD custom **health check** (Lua) that maps
`Workspace.status.phase` to ArgoCD's health model, so Terraform drift shows
up directly in the ArgoCD UI:

| `status.phase` | ArgoCD health |
|----------------|---------------|
| `Synced`       | Healthy       |
| `OutOfSync`    | Degraded      |
| `Failed`       | Degraded      |
| `Planning` / `Applying` | Progressing |
| `Pending`      | Progressing   |

With this installed, teams already on ArgoCD get a single pane of glass:
K8s manifests and Terraform drift visible side by side. See
[issue tracker](https://github.com/tfsync/tfsync/issues) for progress.

## Security notes

- Credentials and backend config are injected via `envFrom: secretRef`.
- `.status.lastPlanOutput` is trimmed and is **not** a trusted audit log — it
  is a convenience field. Prefer container logs for full output.
- The runner Job runs under a dedicated `tfsync-runner` ServiceAccount
  with no extra cluster permissions.

## Project layout

```
api/v1alpha1/              # Workspace CRD types
cmd/manager/               # controller-manager entrypoint
cmd/tfsync/                 # tfsync CLI
internal/controller/       # WorkspaceReconciler
internal/runner/           # runner Job + ConfigMap builder
internal/git/              # shallow clone helper
config/crd/bases/          # CRD manifest
config/rbac/               # ClusterRole / Bindings / SAs
config/manager/            # namespace + controller Deployment
config/samples/            # example Workspace resources
charts/tfsync/          # Helm chart
```

## Development

```sh
make tidy
make build
make run         # runs the controller locally against your current kubecontext
make test
```

## Status

Early alpha. Expect breaking changes until `v1beta1`.
