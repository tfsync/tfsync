// Package runner builds the ephemeral Kubernetes Job and ConfigMap that
// actually execute `terraform init/plan/apply` for a Workspace reconcile.
package runner

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tfsyncv1alpha1 "github.com/tfsync/tfsync/api/v1alpha1"
)

const (
	runnerImage   = "hashicorp/terraform:latest"
	workDir       = "/workspace" // writable emptyDir; terraform runs here
	moduleSrcPath = "/module-src" // read-only ConfigMap mount
	containerName = "terraform"
	labelOwner    = "tfsync.io/workspace"
	labelRunID    = "tfsync.io/run-id"
)

// JobSpec is the intent produced by BuildJob.
type JobSpec struct {
	ConfigMap *corev1.ConfigMap
	Job       *batchv1.Job
}

// Options feeds BuildJob. Apply governs whether `terraform apply` runs after plan.
type Options struct {
	Workspace *tfsyncv1alpha1.Workspace
	RunID     string
	Files     map[string]string
	Apply     bool
}

// BuildJob materialises a ConfigMap carrying the .tf files and a Job that
// runs terraform init + plan (+ apply). The Job owns the ConfigMap so both
// get garbage-collected together.
func BuildJob(o Options) JobSpec {
	ws := o.Workspace
	name := fmt.Sprintf("tfs-%s-%s", ws.Name, o.RunID)

	labels := map[string]string{
		labelOwner: ws.Name,
		labelRunID: o.RunID,
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ws.Namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				ownerRef(ws),
			},
		},
		Data: o.Files,
	}

	script := initPlanScript
	if o.Apply {
		script = initPlanApplyScript
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ws.Namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				ownerRef(ws),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr[int32](0),
			TTLSecondsAfterFinished: ptr[int32](3600),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: "tfsync-runner",
					Containers: []corev1.Container{{
						Name:       containerName,
						Image:      runnerImage,
						WorkingDir: workDir,
						Command:    []string{"/bin/sh", "-c", script},
						EnvFrom:    envFrom(ws),
						VolumeMounts: []corev1.VolumeMount{
							{Name: "workspace", MountPath: workDir},
							{Name: "module", MountPath: moduleSrcPath, ReadOnly: true},
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "workspace",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "module",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: name},
								},
							},
						},
					},
				},
			},
		},
	}

	return JobSpec{ConfigMap: cm, Job: job}
}

// envFrom projects credentials (always) and backend config (optional)
// into the runner as environment variables. Nothing is echoed to logs.
func envFrom(ws *tfsyncv1alpha1.Workspace) []corev1.EnvFromSource {
	out := []corev1.EnvFromSource{}
	if ws.Spec.Credentials != nil && ws.Spec.Credentials.SecretRef != "" {
		out = append(out, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: ws.Spec.Credentials.SecretRef},
			},
		})
	}
	if ws.Spec.Backend.SecretRef != "" {
		out = append(out, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: ws.Spec.Backend.SecretRef},
			},
		})
	}
	return out
}

func ownerRef(ws *tfsyncv1alpha1.Workspace) metav1.OwnerReference {
	t := true
	return metav1.OwnerReference{
		APIVersion:         tfsyncv1alpha1.GroupVersion.String(),
		Kind:               "Workspace",
		Name:               ws.Name,
		UID:                ws.UID,
		Controller:         &t,
		BlockOwnerDeletion: &t,
	}
}

func ptr[T any](v T) *T { return &v }

const initPlanScript = `set -eu
cp -r /module-src/. .
terraform init -input=false -no-color
terraform plan -input=false -no-color -out=tfplan -detailed-exitcode
ec=$?
echo "TFSYNC_PLAN_EXIT=${ec}"
if [ "$ec" = "1" ]; then exit 1; fi
exit 0
`

const initPlanApplyScript = `set -eu
cp -r /module-src/. .
terraform init -input=false -no-color
terraform plan -input=false -no-color -out=tfplan -detailed-exitcode || ec=$?
ec=${ec:-0}
echo "TFSYNC_PLAN_EXIT=${ec}"
if [ "$ec" = "1" ]; then exit 1; fi
if [ "$ec" = "2" ]; then
  terraform apply -input=false -no-color -auto-approve tfplan
fi
exit 0
`
