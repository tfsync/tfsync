// Package gitprovider contains GitProvider implementations.
package gitprovider

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/tfsync/tfsync/internal/provider"
)

// HTTPSProvider clones repositories over HTTPS with optional token auth.
// It handles any HTTPS URL — github.com, gitlab.com, self-hosted GitLab,
// GitHub Enterprise, Bitbucket Server, Gitea, etc. — without special-casing
// any hostname. Auth is driven entirely by the presence of a secretRef.
type HTTPSProvider struct{}

func (HTTPSProvider) Scheme() string { return "https" }

func (HTTPSProvider) Clone(ctx context.Context, req provider.CloneRequest, secrets provider.SecretProvider) (*provider.CloneResult, error) {
	branch := req.Branch
	if branch == "" {
		branch = "main"
	}

	cloneOpts := &gogit.CloneOptions{
		URL:           req.Repo,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		SingleBranch:  true,
		Depth:         1,
		Progress:      io.Discard,
	}

	if req.SecretRef != "" {
		auth, err := buildBasicAuth(ctx, req.Namespace, req.SecretRef, secrets)
		if err != nil {
			return nil, err
		}
		cloneOpts.Auth = auth
	}

	dir, err := os.MkdirTemp("", "tfsync-clone-")
	if err != nil {
		return nil, fmt.Errorf("mkdir temp: %w", err)
	}

	repo, err := gogit.PlainCloneContext(ctx, dir, false, cloneOpts)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("git clone: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("resolve HEAD: %w", err)
	}

	root := filepath.Join(dir, filepath.Clean("/"+req.Path))
	files, err := collectTerraform(root, dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if len(files) == 0 {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("no .tf files found at path %q", req.Path)
	}

	return &provider.CloneResult{Dir: dir, SHA: head.Hash().String(), Files: files}, nil
}

// buildBasicAuth fetches the named secret and returns go-git BasicAuth.
// The secret must contain a "token" key. An optional "username" key overrides
// the default ("git"), which is accepted by GitHub, GitLab, GHE, Bitbucket,
// and most self-hosted HTTPS git servers.
func buildBasicAuth(ctx context.Context, namespace, secretRef string, secrets provider.SecretProvider) (*gogithttp.BasicAuth, error) {
	data, err := secrets.GetSecret(ctx, namespace, secretRef)
	if err != nil {
		return nil, fmt.Errorf("fetch auth secret %q: %w", secretRef, err)
	}
	token, ok := data["token"]
	if !ok || token == "" {
		return nil, fmt.Errorf("auth secret %q missing required key \"token\"", secretRef)
	}
	username := data["username"]
	if username == "" {
		username = "git"
	}
	return &gogithttp.BasicAuth{Username: username, Password: token}, nil
}

func collectTerraform(root, base string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		if !strings.HasSuffix(name, ".tf") && !strings.HasSuffix(name, ".tfvars") && name != "backend.tf.json" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	return out, nil
}
