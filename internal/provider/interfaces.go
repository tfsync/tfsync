package provider

import "context"

// CloneResult holds the on-disk clone location, resolved commit SHA, and
// the Terraform files extracted from the repo.
type CloneResult struct {
	Dir   string
	SHA   string
	Files map[string]string
}

// CloneRequest carries everything a GitProvider needs from the Workspace spec.
type CloneRequest struct {
	Repo      string
	Branch    string
	Path      string
	SecretRef string // "" means unauthenticated
	Namespace string // namespace of the Workspace, used to resolve SecretRef
}

// GitProvider clones a repository and returns its Terraform files.
// Implementations are selected by URL scheme — never by hostname.
type GitProvider interface {
	Scheme() string
	Clone(ctx context.Context, req CloneRequest, secrets SecretProvider) (*CloneResult, error)
}

// SecretProvider fetches a Kubernetes Secret's data by namespace and name.
type SecretProvider interface {
	GetSecret(ctx context.Context, namespace, name string) (map[string]string, error)
}

// StateBackend renders optional backend configuration for the runner Job.
// Implementations are selected by the Workspace's backend.type field.
type StateBackend interface {
	Type() string
	// ConfigureBackendFile returns a backend.tf.json to inject into the
	// runner ConfigMap. Empty string means no injection needed.
	ConfigureBackendFile(ctx context.Context, secrets map[string]string) (string, error)
}
