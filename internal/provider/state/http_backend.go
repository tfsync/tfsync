package stateprovider

import (
	"context"
	"fmt"
)

// HTTPBackend is a placeholder for the Terraform HTTP remote state backend.
// This covers GitLab managed Terraform state (which uses HTTP under the hood).
// TODO: implement — required secret keys: address, username (optional), password (optional), lock_address (optional), unlock_address (optional)
type HTTPBackend struct{}

func (HTTPBackend) Type() string { return "http" }

func (HTTPBackend) ConfigureBackendFile(_ context.Context, _ map[string]string) (string, error) {
	return "", fmt.Errorf("http state backend not yet implemented")
}
