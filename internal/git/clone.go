// Package git clones a Terraform module out of a Git repository onto local
// disk so the controller can slice it into a ConfigMap for the runner Job.
package git

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// CloneOptions controls a shallow clone of a single branch.
type CloneOptions struct {
	Repo   string
	Branch string
	Path   string // subdirectory within the repo to extract; "" == root
}

// Result holds the on-disk clone location and resolved commit SHA.
type Result struct {
	Dir    string
	SHA    string
	Files  map[string]string // relative path -> contents (only files under Path)
}

// Clone performs a shallow clone of opts.Repo at opts.Branch into a temp dir.
// The returned Result.Files only contains *.tf / *.tfvars under opts.Path.
// Callers are responsible for removing Result.Dir.
func Clone(ctx context.Context, opts CloneOptions) (*Result, error) {
	if opts.Branch == "" {
		opts.Branch = "main"
	}
	dir, err := os.MkdirTemp("", "tfsync-clone-")
	if err != nil {
		return nil, fmt.Errorf("mkdir temp: %w", err)
	}

	repo, err := gogit.PlainCloneContext(ctx, dir, false, &gogit.CloneOptions{
		URL:           opts.Repo,
		ReferenceName: plumbing.NewBranchReferenceName(opts.Branch),
		SingleBranch:  true,
		Depth:         1,
		Progress:      io.Discard,
	})
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("git clone: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("resolve HEAD: %w", err)
	}

	root := filepath.Join(dir, filepath.Clean("/"+opts.Path))
	files, err := collectTerraform(root, dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if len(files) == 0 {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("no .tf files found at path %q", opts.Path)
	}

	return &Result{Dir: dir, SHA: head.Hash().String(), Files: files}, nil
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
