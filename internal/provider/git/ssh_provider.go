package gitprovider

import (
	"context"
	"fmt"

	"github.com/tfsync/tfsync/internal/provider"
)

// SSHProvider is a stub for git@ / ssh:// URLs. Not yet implemented.
type SSHProvider struct{}

func (SSHProvider) Scheme() string { return "ssh" }

func (SSHProvider) Clone(_ context.Context, req provider.CloneRequest, _ provider.SecretProvider) (*provider.CloneResult, error) {
	return nil, fmt.Errorf("ssh git provider not yet implemented for repo %q", req.Repo)
}
