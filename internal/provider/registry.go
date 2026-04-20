package provider

import (
	"fmt"
	"net/url"
	"strings"
)

// Registry dispatches to the right GitProvider by URL scheme and the right
// StateBackend by backend type. Construct once in main and inject into the
// reconciler. All lookups return errors rather than panicking so the
// controller can surface them as PhaseFailed.
type Registry struct {
	gitProviders  map[string]GitProvider
	stateBackends map[string]StateBackend
	SecretProvider SecretProvider
}

func NewRegistry(sp SecretProvider) *Registry {
	return &Registry{
		gitProviders:   make(map[string]GitProvider),
		stateBackends:  make(map[string]StateBackend),
		SecretProvider: sp,
	}
}

func (r *Registry) RegisterGit(p GitProvider) {
	scheme := p.Scheme()
	if _, exists := r.gitProviders[scheme]; exists {
		panic(fmt.Sprintf("provider: duplicate GitProvider for scheme %q", scheme))
	}
	r.gitProviders[scheme] = p
}

func (r *Registry) RegisterState(b StateBackend) {
	r.stateBackends[b.Type()] = b
}

// GitProviderFor returns the GitProvider for rawURL's scheme.
// SCP-style git addresses (git@host:org/repo) are mapped to scheme "ssh".
func (r *Registry) GitProviderFor(rawURL string) (GitProvider, error) {
	scheme, err := extractScheme(rawURL)
	if err != nil {
		return nil, err
	}
	p, ok := r.gitProviders[scheme]
	if !ok {
		return nil, fmt.Errorf("provider: no GitProvider registered for scheme %q (url: %s)", scheme, rawURL)
	}
	return p, nil
}

// StateBackendFor returns the StateBackend for backendType, or nil if none
// is registered (acceptable — the runner uses whatever backend.tf the user
// committed to the repo).
func (r *Registry) StateBackendFor(backendType string) StateBackend {
	return r.stateBackends[backendType]
}

// extractScheme normalises the URL scheme. SCP-style git addresses
// (git@github.com:org/repo.git) are not valid URLs but are common;
// we detect them by the presence of "@" without "://" and map to "ssh".
func extractScheme(rawURL string) (string, error) {
	if !strings.Contains(rawURL, "://") && strings.Contains(rawURL, "@") {
		return "ssh", nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("provider: parse url %q: %w", rawURL, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "git+ssh" {
		scheme = "ssh"
	}
	return scheme, nil
}
