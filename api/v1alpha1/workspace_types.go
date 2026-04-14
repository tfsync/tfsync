package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkspacePhase is the observed reconciliation state of a Workspace.
type WorkspacePhase string

const (
	PhasePending      WorkspacePhase = "Pending"
	PhaseInitializing WorkspacePhase = "Initializing"
	PhasePlanning     WorkspacePhase = "Planning"
	PhaseApplying     WorkspacePhase = "Applying"
	PhaseSynced       WorkspacePhase = "Synced"
	PhaseFailed       WorkspacePhase = "Failed"
	PhaseOutOfSync    WorkspacePhase = "OutOfSync"
)

// BackendType enumerates supported terraform remote state backends.
// +kubebuilder:validation:Enum=s3;gcs;azurerm;local
type BackendType string

type GitSource struct {
	// Repo is the Git repository URL (https or ssh).
	Repo string `json:"repo"`

	// Path within the repo that contains the .tf files.
	// +optional
	Path string `json:"path,omitempty"`

	// Branch to track. Defaults to "main".
	// +kubebuilder:default=main
	// +optional
	Branch string `json:"branch,omitempty"`
}

type SyncPolicy struct {
	// AutoApply runs `terraform apply` automatically after a successful plan.
	// When false, the workspace parks in OutOfSync after a plan with changes.
	// +optional
	AutoApply bool `json:"autoApply,omitempty"`

	// Interval between reconciliations / drift checks.
	// +kubebuilder:default="5m"
	// +optional
	Interval metav1.Duration `json:"interval,omitempty"`
}

type BackendConfig struct {
	// Type of remote state backend.
	Type BackendType `json:"type"`

	// SecretRef points at a Secret with backend-specific configuration
	// (bucket, region, key, etc.) exposed as env vars / backend.tf.json.
	// +optional
	SecretRef string `json:"secretRef,omitempty"`
}

type CredentialsRef struct {
	// SecretRef is the name of a Secret holding cloud provider credentials.
	// All keys are projected into the runner Job as env vars.
	SecretRef string `json:"secretRef"`
}

// WorkspaceSpec defines the desired state of a Terraform workspace.
type WorkspaceSpec struct {
	Source      GitSource      `json:"source"`
	SyncPolicy  SyncPolicy     `json:"syncPolicy,omitempty"`
	Backend     BackendConfig  `json:"backend"`
	Credentials CredentialsRef `json:"credentials"`
}

// WorkspaceStatus reports the observed state of a Workspace.
type WorkspaceStatus struct {
	// Phase is the high-level lifecycle state.
	// +optional
	Phase WorkspacePhase `json:"phase,omitempty"`

	// LastAppliedAt is the timestamp of the most recent successful apply.
	// +optional
	LastAppliedAt *metav1.Time `json:"lastAppliedAt,omitempty"`

	// LastPlanOutput is a trimmed summary of the most recent plan.
	// Secrets are never written here.
	// +optional
	LastPlanOutput string `json:"lastPlanOutput,omitempty"`

	// ObservedGeneration reflects the .metadata.generation this status was computed from.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ActiveJob is the name of the currently running runner Job, if any.
	// +optional
	ActiveJob string `json:"activeJob,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ws,categories=tfsync
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Repo",type=string,JSONPath=`.spec.source.repo`
// +kubebuilder:printcolumn:name="Branch",type=string,JSONPath=`.spec.source.branch`
// +kubebuilder:printcolumn:name="LastApplied",type=date,JSONPath=`.status.lastAppliedAt`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Workspace binds a Git-hosted Terraform module to a set of cloud credentials
// and sync behavior. The controller reconciles spec -> infrastructure.
type Workspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkspaceSpec   `json:"spec,omitempty"`
	Status WorkspaceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type WorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workspace `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Workspace{}, &WorkspaceList{})
}
