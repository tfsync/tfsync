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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tfsyncv1alpha1 "github.com/tfsync/tfsync/api/v1alpha1"
	"github.com/tfsync/tfsync/internal/git"
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
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=tfsync.io,resources=workspaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tfsync.io,resources=workspaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tfsync.io,resources=workspaces/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *WorkspaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("workspace", req.NamespacedName)

	var ws tfsyncv1alpha1.Workspace
	if err := r.Get(ctx, req.NamespacedName, &ws); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

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

	if err := r.setPhase(ctx, &ws, tfsyncv1alpha1.PhaseInitializing, "Cloning", "cloning git repo"); err != nil {
		return ctrl.Result{}, err
	}

	cloneCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	res, err := git.Clone(cloneCtx, git.CloneOptions{
		Repo:   ws.Spec.Source.Repo,
		Branch: ws.Spec.Source.Branch,
		Path:   ws.Spec.Source.Path,
	})
	if err != nil {
		return r.fail(ctx, &ws, fmt.Sprintf("git clone failed: %v", err))
	}
	defer os.RemoveAll(res.Dir)

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
	if err := r.setPhase(ctx, &ws, phase, "Running", fmt.Sprintf("runner job %s started", jobSpec.Job.Name)); err != nil {
		return ctrl.Result{}, err
	}

	// Poll the runner; longer requeue than the in-flight path because this
	// is the first observation.
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// observeActiveJob inspects the named Job. Returns done=true when the Job
// has succeeded or failed; writes terminal status back to the Workspace.
func (r *WorkspaceReconciler) observeActiveJob(ctx context.Context, ws *tfsyncv1alpha1.Workspace) (bool, error) {
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
		return true, r.setPhase(ctx, ws, phase, "RunnerSucceeded", msg)

	case job.Status.Failed > 0:
		logs := r.fetchJobLogs(ctx, &job)
		ws.Status.LastPlanOutput = trim(logs, planOutputMax)
		ws.Status.ActiveJob = ""
		return true, r.setPhase(ctx, ws, tfsyncv1alpha1.PhaseFailed, "RunnerFailed", "runner job failed")
	}

	return false, nil
}

// fetchJobLogs is a best-effort pull of the runner's stdout. The controller
// cannot reach the pod log API via cached client; callers treat empty as "no output".
// A production implementation would use a direct clientset here.
func (r *WorkspaceReconciler) fetchJobLogs(ctx context.Context, job *batchv1.Job) string {
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(job.Namespace), client.MatchingLabels{"job-name": job.Name}); err != nil {
		return ""
	}
	if len(pods.Items) == 0 {
		return ""
	}
	// Terminal message from the container is a cheap proxy without clientset wiring.
	for _, s := range pods.Items[0].Status.ContainerStatuses {
		if s.State.Terminated != nil && s.State.Terminated.Message != "" {
			return s.State.Terminated.Message
		}
	}
	return ""
}

func (r *WorkspaceReconciler) fail(ctx context.Context, ws *tfsyncv1alpha1.Workspace, msg string) (ctrl.Result, error) {
	_ = r.setPhase(ctx, ws, tfsyncv1alpha1.PhaseFailed, "Error", msg)
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *WorkspaceReconciler) setPhase(ctx context.Context, ws *tfsyncv1alpha1.Workspace, phase tfsyncv1alpha1.WorkspacePhase, reason, msg string) error {
	ws.Status.Phase = phase
	ws.Status.ObservedGeneration = ws.Generation
	meta.SetStatusCondition(&ws.Status.Conditions, metav1.Condition{
		Type:    "Ready",
		Status:  readyFromPhase(phase),
		Reason:  reason,
		Message: msg,
	})
	return r.Status().Update(ctx, ws)
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
func (r *WorkspaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tfsyncv1alpha1.Workspace{}).
		Owns(&batchv1.Job{}, builder.WithPredicates()).
		Owns(&corev1.ConfigMap{}).
		Watches(
			&batchv1.Job{},
			handler.EnqueueRequestsFromMapFunc(jobToWorkspace),
		).
		Complete(r)
}

func jobToWorkspace(_ context.Context, obj client.Object) []reconcile.Request {
	owner := obj.GetLabels()["tfsync.io/workspace"]
	if owner == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: obj.GetNamespace(), Name: owner}}}
}
