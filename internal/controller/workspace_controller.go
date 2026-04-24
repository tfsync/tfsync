// Package controller implements the Workspace reconciler: clone Git, spawn
// a terraform runner Job, transition status, requeue on the sync interval.
package controller

import (
	"context"
	"fmt"
	"os"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	tfsyncv1alpha1 "github.com/tfsync/tfsync/api/v1alpha1"
	"github.com/tfsync/tfsync/internal/provider"
	"github.com/tfsync/tfsync/internal/runner"
)

const (
	defaultInterval = 5 * time.Minute
	finalizerName   = "tfsync.io/finalizer"
	// planOutputMax caps what we copy into status to avoid unbounded growth.
	planOutputMax = 4096
)

// WorkspaceReconciler reconciles Workspace objects.
type WorkspaceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Registry *provider.Registry
	// Clientset is a direct (non-cached) kubernetes client used for subresources
	// the controller-runtime cached client cannot serve — notably pods/log.
	Clientset kubernetes.Interface
}

// +kubebuilder:rbac:groups=tfsync.io,resources=workspaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tfsync.io,resources=workspaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tfsync.io,resources=workspaces/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *WorkspaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("workspace", req.NamespacedName)

	var ws tfsyncv1alpha1.Workspace
	if err := r.Get(ctx, req.NamespacedName, &ws); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Capture before any status mutations so all field changes made in this
	// reconcile are included in setPhase's merge patch.
	base := ws.DeepCopy()

	interval := ws.Spec.SyncPolicy.Interval.Duration
	if interval <= 0 {
		interval = defaultInterval
	}

	// If a runner Job is in flight, observe it and bail until it finishes.
	if ws.Status.ActiveJob != "" {
		done, err := r.observeActiveJob(ctx, &ws)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !done {
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		// Job completed — fall through to re-evaluate.
	}

	// Skip re-reconcile until interval has elapsed since last apply/plan.
	if ws.Status.Phase == tfsyncv1alpha1.PhaseSynced && ws.Status.LastAppliedAt != nil {
		since := time.Since(ws.Status.LastAppliedAt.Time)
		if since < interval {
			return ctrl.Result{RequeueAfter: interval - since}, nil
		}
	}

	logger.Info("starting reconcile", "phase", ws.Status.Phase)

	if err := r.setPhase(ctx, base, &ws, tfsyncv1alpha1.PhaseInitializing, "Cloning", "cloning git repo"); err != nil {
		return ctrl.Result{}, err
	}

	gitProv, err := r.Registry.GitProviderFor(ws.Spec.Source.Repo)
	if err != nil {
		return r.fail(ctx, &ws, fmt.Sprintf("no git provider: %v", err))
	}

	cloneCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	res, err := gitProv.Clone(cloneCtx, provider.CloneRequest{
		Repo:      ws.Spec.Source.Repo,
		Branch:    ws.Spec.Source.Branch,
		Path:      ws.Spec.Source.Path,
		SecretRef: ws.Spec.Source.SecretRef,
		Namespace: ws.Namespace,
	}, r.Registry.SecretProvider)
	if err != nil {
		return r.fail(ctx, &ws, fmt.Sprintf("git clone failed: %v", err))
	}
	defer os.RemoveAll(res.Dir)

	// Inject backend.tf.json if the registered state backend generates one.
	// This overwrites any backend.tf.json the user committed, so they don't
	// need to store backend config in the repo.
	if backend := r.Registry.StateBackendFor(string(ws.Spec.Backend.Type)); backend != nil {
		var secretData map[string]string
		if ws.Spec.Backend.SecretRef != "" {
			secretData, err = r.Registry.SecretProvider.GetSecret(ctx, ws.Namespace, ws.Spec.Backend.SecretRef)
			if err != nil {
				return r.fail(ctx, &ws, fmt.Sprintf("fetch backend secret: %v", err))
			}
		}
		backendConfig, err := backend.ConfigureBackendFile(ctx, secretData)
		if err != nil {
			return r.fail(ctx, &ws, fmt.Sprintf("configure backend: %v", err))
		}
		if backendConfig != "" {
			res.Files["backend.tf.json"] = backendConfig
		}
	}

	apply := ws.Spec.SyncPolicy.AutoApply
	phase := tfsyncv1alpha1.PhasePlanning
	if apply {
		phase = tfsyncv1alpha1.PhaseApplying
	}

	runID := res.SHA[:min(7, len(res.SHA))] + "-" + shortTS()
	jobSpec := runner.BuildJob(runner.Options{
		Workspace: &ws,
		RunID:     runID,
		Files:     res.Files,
		Apply:     apply,
	})

	if err := r.Create(ctx, jobSpec.ConfigMap); err != nil && !apierrors.IsAlreadyExists(err) {
		return r.fail(ctx, &ws, fmt.Sprintf("create configmap: %v", err))
	}
	if err := r.Create(ctx, jobSpec.Job); err != nil && !apierrors.IsAlreadyExists(err) {
		return r.fail(ctx, &ws, fmt.Sprintf("create job: %v", err))
	}

	ws.Status.ActiveJob = jobSpec.Job.Name
	if err := r.setPhase(ctx, base, &ws, phase, "Running", fmt.Sprintf("runner job %s started", jobSpec.Job.Name)); err != nil {
		return ctrl.Result{}, err
	}

	// Poll the runner; longer requeue than the in-flight path because this
	// is the first observation.
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// observeActiveJob inspects the named Job. Returns done=true when the Job
// has succeeded or failed; writes terminal status back to the Workspace.
func (r *WorkspaceReconciler) observeActiveJob(ctx context.Context, ws *tfsyncv1alpha1.Workspace) (bool, error) {
	base := ws.DeepCopy()

	var job batchv1.Job
	key := types.NamespacedName{Namespace: ws.Namespace, Name: ws.Status.ActiveJob}
	if err := r.Get(ctx, key, &job); err != nil {
		if apierrors.IsNotFound(err) {
			// Job already gc'd — clear and retry.
			ws.Status.ActiveJob = ""
			return true, r.Status().Update(ctx, ws)
		}
		return false, err
	}

	switch {
	case job.Status.Succeeded > 0:
		planOutput := r.fetchJobLogs(ctx, &job)
		now := metav1.NewTime(time.Now())
		ws.Status.LastAppliedAt = &now
		ws.Status.LastPlanOutput = trim(planOutput, planOutputMax)
		ws.Status.ActiveJob = ""

		phase := tfsyncv1alpha1.PhaseSynced
		msg := "apply succeeded"
		// With AutoApply=false, Planning completes as OutOfSync when changes exist.
		if !ws.Spec.SyncPolicy.AutoApply && containsChanges(planOutput) {
			phase = tfsyncv1alpha1.PhaseOutOfSync
			msg = "plan completed; manual approval required"
		}
		return true, r.setPhase(ctx, base, ws, phase, "RunnerSucceeded", msg)

	case job.Status.Failed > 0:
		logs := r.fetchJobLogs(ctx, &job)
		ws.Status.LastPlanOutput = trim(logs, planOutputMax)
		ws.Status.ActiveJob = ""
		return true, r.setPhase(ctx, base, ws, tfsyncv1alpha1.PhaseFailed, "RunnerFailed", "runner job failed")
	}

	return false, nil
}

// fetchJobLogs streams the runner pod's terraform container stdout using a
// direct clientset (the cached controller-runtime client cannot serve
// subresources like pods/log). Returns empty on any failure — callers treat
// that as "no output" and rely on the Job.Status for success/failure signal.
func (r *WorkspaceReconciler) fetchJobLogs(ctx context.Context, job *batchv1.Job) string {
	if r.Clientset == nil {
		return ""
	}
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(job.Namespace), client.MatchingLabels{"job-name": job.Name}); err != nil {
		return ""
	}
	if len(pods.Items) == 0 {
		return ""
	}
	req := r.Clientset.CoreV1().Pods(job.Namespace).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{
		Container: "terraform",
	})
	out, err := req.DoRaw(ctx)
	if err != nil {
		return ""
	}
	return string(out)
}

func (r *WorkspaceReconciler) fail(ctx context.Context, ws *tfsyncv1alpha1.Workspace, msg string) (ctrl.Result, error) {
	_ = r.setPhase(ctx, ws.DeepCopy(), ws, tfsyncv1alpha1.PhaseFailed, "Error", msg)
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// setPhase writes phase + Ready condition to .status via a merge patch.
// base must be a DeepCopy of ws captured BEFORE any status mutations in the
// current reconcile — that way all mutated fields are included in the patch.
func (r *WorkspaceReconciler) setPhase(ctx context.Context, base, ws *tfsyncv1alpha1.Workspace, phase tfsyncv1alpha1.WorkspacePhase, reason, msg string) error {
	patch := client.MergeFrom(base)
	ws.Status.Phase = phase
	ws.Status.ObservedGeneration = ws.Generation
	meta.SetStatusCondition(&ws.Status.Conditions, metav1.Condition{
		Type:    "Ready",
		Status:  readyFromPhase(phase),
		Reason:  reason,
		Message: msg,
	})
	return r.Status().Patch(ctx, ws, patch)
}

func readyFromPhase(p tfsyncv1alpha1.WorkspacePhase) metav1.ConditionStatus {
	switch p {
	case tfsyncv1alpha1.PhaseSynced:
		return metav1.ConditionTrue
	case tfsyncv1alpha1.PhaseFailed:
		return metav1.ConditionFalse
	default:
		return metav1.ConditionUnknown
	}
}

// containsChanges is a rough heuristic on terraform plan output.
func containsChanges(out string) bool {
	return contains(out, "Plan:") && !contains(out, "No changes")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func shortTS() string {
	return time.Now().UTC().Format("150405")
}

// SetupWithManager wires the reconciler into the manager and watches owned Jobs.
// GenerationChangedPredicate on the Workspace avoids a reconcile storm where
// our own status writes trigger watch events that trigger more status writes.
// Owns(Job/ConfigMap) still fires on owned-resource changes via ownerRefs.
func (r *WorkspaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tfsyncv1alpha1.Workspace{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&batchv1.Job{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
