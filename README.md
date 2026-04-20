<p align="center">
  <h1 align="center">TFSync</h1>
  <p align="center">
    GitOps-native Terraform operator for Kubernetes.
    <br />
    ArgoCD, but for Terraform. Declare a Workspace CR — the cluster does the rest.
  </p>
</p>

<p align="center">
  <a href="https://github.com/tfsync/tfsync/releases"><img src="https://img.shields.io/github/v/release/tfsync/tfsync" alt="Latest Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT License"></a>
  <a href="https://github.com/tfsync/tfsync/stargazers"><img src="https://img.shields.io/github/stars/tfsync/tfsync?style=social" alt="GitHub Stars"></a>
</p>

---

Welcome to the repository for [TFSync](https://tfsync.github.io/tfsync/) — a GitOps-native Terraform operator for Kubernetes. This repo contains the source for the tfsync controller, the `tfsync` CLI, a Helm chart, and CRD manifests. Please review the [Why tfsync?](#why-tfsync) section below for more details.

---

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

---

## Why tfsync?

**GitOps works great for Kubernetes manifests.** ArgoCD, Flux, and friends watch a Git repo and reconcile cluster state automatically. But Terraform sits outside that loop — someone still has to run `terraform apply` manually, in CI, or with a long-running agent that accumulates shared state.

tfsync closes that gap by treating Terraform workspaces as Kubernetes resources:

- **GitOps all the way down** — infrastructure follows the same pull-based reconcile pattern as the rest of your cluster
- **No long-running agents** — each reconcile spawns a fresh, ephemeral runner Job — no shared state, no lingering processes
- **Drift detection** — the operator re-plans on the sync interval and flips a workspace to `OutOfSync` when state drifts from Git
- **Cloud-native credentials** — provider and backend secrets are plain Kubernetes Secrets, projected into the runner Job — never written to status or logs

---

## Table of Contents

- [Features](#features)
- [Quick Start](#quick-start)
- [Workspace Lifecycle](#workspace-lifecycle)
- [CLI Reference](#cli-reference)
- [ArgoCD Integration](#argocd-integration-planned)
- [Project Structure](#project-structure)
- [Development](#development)
- [Security](#security)

---

## Features

- **Workspace CRD** — declare Terraform workspaces as first-class Kubernetes resources
- **Ephemeral runner Jobs** — each reconcile spawns an isolated `hashicorp/terraform` Job; no shared state between runs
- **Drift detection** — automatic re-plan on a configurable interval; workspace flips to `OutOfSync` on drift
- **Auto-apply mode** — set `autoApply: true` to apply changes automatically; leave it off to require manual approval
- **Flexible backends** — S3, GCS, Azure Blob, or any Terraform backend — config lives in a Kubernetes Secret
- **Cloud-native credentials** — AWS, GCP, Azure credentials injected via `envFrom: secretRef`, never logged or written to status
- **Helm chart** — production-ready chart with RBAC, namespace, and controller Deployment
- **`tfsync` CLI** — list workspaces, trigger plans, and force reconciles from your terminal
- **ArgoCD-composable** — `Workspace` is a standard CRD; ArgoCD can sync it like any manifest (health check coming)

---

## Quick Start

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

---

## Workspace Lifecycle

| Phase | Meaning |
|-------|---------|
| `Pending` | Reconcile queued, no work started yet |
| `Initializing` | Git clone in progress |
| `Planning` | Runner Job executing `terraform plan` |
| `Applying` | Runner Job executing `terraform apply` (`autoApply: true`) |
| `OutOfSync` | Plan shows drift; waiting for manual approval |
| `Synced` | Infrastructure matches Git; next check at `syncPolicy.interval` |
| `Failed` | Clone or runner Job failed; see `.status.lastPlanOutput` |

---

## CLI Reference

```sh
make build-cli

./bin/tfsync list -n default
./bin/tfsync plan example-aws-vpc -n default
./bin/tfsync sync example-aws-vpc -n default   # triggers immediate reconcile
```

| Command | Description |
|---------|-------------|
| `tfsync list` | List all Workspace resources in a namespace |
| `tfsync plan <name>` | Show the last plan output for a workspace |
| `tfsync sync <name>` | Trigger an immediate reconcile |

---

## ArgoCD Integration (planned)

tfsync is designed to compose with ArgoCD rather than replace it. Because `Workspace` is a regular Kubernetes CRD, ArgoCD can sync it like any other manifest — the full GitOps story looks like:

```
┌──────────────────┐    syncs CRs    ┌────────────────────┐   reconciles    ┌──────────────┐
│ Git (Workspace   │ ──────────────▶ │ Kubernetes cluster │ ──────────────▶ │ Cloud infra  │
│ YAMLs)           │    (ArgoCD)     │ (Workspace CRs)    │    (tfsync)     │              │
└──────────────────┘                 └────────────────────┘                 └──────────────┘
```

A future release will ship an ArgoCD custom health check (Lua) that maps `Workspace.status.phase` to ArgoCD's health model, so Terraform drift shows up directly in the ArgoCD UI:

| `status.phase` | ArgoCD health |
|----------------|---------------|
| `Synced` | Healthy |
| `OutOfSync` | Degraded |
| `Failed` | Degraded |
| `Planning` / `Applying` | Progressing |
| `Pending` | Progressing |

With this installed, teams already on ArgoCD get a single pane of glass: Kubernetes manifests and Terraform drift visible side by side. See the [issue tracker](https://github.com/tfsync/tfsync/issues) for progress.

---

## Project Structure

```
tfsync/
├── api/v1alpha1/              # Workspace CRD types
├── cmd/manager/               # controller-manager entrypoint
├── cmd/tfsync/                # tfsync CLI
├── internal/controller/       # WorkspaceReconciler
├── internal/runner/           # runner Job + ConfigMap builder
├── internal/git/              # shallow clone helper
├── config/crd/bases/          # CRD manifest
├── config/rbac/               # ClusterRole / Bindings / SAs
├── config/manager/            # namespace + controller Deployment
├── config/samples/            # example Workspace resources
└── charts/tfsync/             # Helm chart
```

---

## Development

```sh
make tidy
make build
make run         # runs the controller locally against your current kubecontext
make test
```

> **Status:** Early alpha (`v1alpha1`). The CRD API and controller behaviour may change between any release without a deprecation period. Pin to a specific version in production and review the changelog before upgrading. The API will stabilise at `v1beta1`.

---

## Security

- Credentials and backend config are injected via `envFrom: secretRef` — never hardcoded in the Workspace spec
- `.status.lastPlanOutput` is trimmed and is **not** a trusted audit log — it is a convenience field; prefer container logs for full output
- The runner Job runs under a dedicated `tfsync-runner` ServiceAccount with no extra cluster permissions

---

*Apache 2.0 License. See [LICENSE](LICENSE) for details.*
