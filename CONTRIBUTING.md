# Contributing to tfsync

Thanks for wanting to contribute! tfsync is early-stage, so your feedback
and patches have outsized impact.

## Reporting issues

- Bugs: please include reproduction steps, Kubernetes version, and a copy
  of the relevant `Workspace` spec (redact secrets).
- Feature requests: describe the problem before the solution. What are you
  trying to do that tfsync makes hard today?

## Development setup

```sh
go mod tidy
make build         # builds the manager
make build-cli     # builds the tfsync CLI
make run           # runs the manager locally against your current kubecontext
make test
```

You'll want a local cluster for manual testing. [kind](https://kind.sigs.k8s.io/)
works well:

```sh
kind create cluster --name tfsync-dev
make install        # applies the CRD
make run             # runs the controller in the foreground
```

## Pull requests

- Keep PRs focused. One feature or fix per PR makes review tractable.
- Run `go fmt ./...` and `go vet ./...` before pushing.
- If you're adding a new provider (Git, backend, or secret), also update
  `README.md` with the supported list.
- Reference any related issue in the PR description.

## Commit style

Short, imperative, lowercase subject line. Examples:

```
feat: gitlab provider with token auth
fix: handle empty plan output on first run
docs: clarify s3 backend secret schema
```

## Code of Conduct

This project follows the
[Contributor Covenant](./CODE_OF_CONDUCT.md). Be kind; assume good intent.

## License

By contributing, you agree that your contributions will be licensed under
the [Apache License 2.0](./LICENSE).
