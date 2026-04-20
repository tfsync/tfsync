// Package stateprovider contains StateBackend implementations.
package stateprovider

import "context"

// NoopBackend satisfies StateBackend for backends that need no dynamic config
// injection. Used for "local" today; S3/GCS/azurerm will be real implementations.
type NoopBackend struct {
	backendType string
}

func NewNoopBackend(t string) *NoopBackend { return &NoopBackend{backendType: t} }

func (b *NoopBackend) Type() string { return b.backendType }

func (b *NoopBackend) ConfigureBackendFile(_ context.Context, _ map[string]string) (string, error) {
	return "", nil
}
