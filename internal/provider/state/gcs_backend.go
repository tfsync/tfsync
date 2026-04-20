package stateprovider

import (
	"context"
	"fmt"
)

// GCSBackend is a placeholder for the GCS remote state backend.
// TODO: implement — required secret keys: bucket, prefix (optional), credentials_file (optional)
type GCSBackend struct{}

func (GCSBackend) Type() string { return "gcs" }

func (GCSBackend) ConfigureBackendFile(_ context.Context, _ map[string]string) (string, error) {
	return "", fmt.Errorf("gcs state backend not yet implemented")
}
