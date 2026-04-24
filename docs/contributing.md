# Contributing

## Development setup

```bash
git clone https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced
cd gitlab-enhanced
make dev-setup   # installs pre-commit hooks and checks Go/Ansible tooling
```

Requirements:
- Go 1.25+
- Ansible 2.15+ (for playbook linting)
- shellcheck (for shell script linting)
- yamllint (for YAML linting)
- pre-commit (`pip install pre-commit`)

## Common tasks

```bash
make build       # compile the binary
make test        # run all Go tests
make lint        # run go vet, shellcheck, yamllint
make fmt         # gofmt + goimports
make package     # build .deb and .rpm packages (requires nfpm)
make release     # tag and push a release (maintainers only)
```

## Project layout

```
abstraction/    Go interfaces for storage, build, runner, environment, config
bandwidth/      Bandwidth proxy service (compression, LFS dedup, artifact policies)
cmd/            CLI entry point (cobra commands)
config/         YAML configuration files (defaults.yaml, local.yaml.example)
deploy/         Ansible playbooks and bootstrap scripts
docs/           Documentation
environments/   Workspace environment subtrees (supervisor, workspace-images)
lfs/            LFS server abstraction
packaging/      nfpm package definitions and systemd units
rewards/        BAT rewards service
runtime/        Incus runner, K8s-in-Incus, CI base image
store/          Shared SQLite persistence layer
tools/          Standalone tools (linux2ipfs, adblock-proxy)
utils/          Utility scripts and tools
```

## Branching

- `main` — always releasable; protected, requires passing CI
- `feature/*` — feature branches; open an MR to merge
- `fix/*` — bug fix branches

Branch names should be lowercase with hyphens: `feature/my-feature`.

## Commit messages

Follow the [Conventional Commits](https://www.conventionalcommits.org/) format:

```
<type>: <short description>

<optional body>
```

Types: `feat`, `fix`, `ci`, `docs`, `refactor`, `test`, `chore`

Examples:
```
feat: add Prometheus metrics endpoint to bandwidth service
fix: return MkdirAll error from DeduplicateLFSObject
docs: add cloud-secondary deployment guide
```

## Testing

```bash
# Run all tests
make test

# Run a specific package
go test ./rewards/... -v

# Run integration tests only
go test ./rewards/... -run TestIntegration -v

# Run with race detector
go test -race ./...
```

All new code must have tests. Integration tests that spin up real HTTP servers
and SQLite databases are preferred over mocks for service-level behaviour.

## Adding a new storage backend

1. Implement the `storage.Backend` interface in `abstraction/storage/`
2. Add a case to `abstraction/storage/registry.go` `FromConfig()`
3. Add the backend name to `config/defaults.yaml` documentation
4. Add tests in `abstraction/storage/<backend>_test.go`
5. Document in `docs/configuration.md`

## Adding a new config field

1. Add the field to the appropriate struct in `abstraction/config/loader.go`
2. Add a string/bool/int/float override in `applyEnvOverrides()`
3. Add the field with a comment to `config/defaults.yaml`
4. Add it to `config/local.yaml.example` (commented out)
5. Document in `docs/configuration.md`

## Merge request checklist

- [ ] `make test` passes locally
- [ ] `make lint` passes locally
- [ ] New config fields documented in `docs/configuration.md`
- [ ] New features documented in the relevant `docs/` file
- [ ] Commit messages follow Conventional Commits format
- [ ] MR description explains the motivation, not just the implementation

## Release process

Releases are tagged from `main` by maintainers:

```bash
make release VERSION=1.2.3
```

This tags the commit, builds packages, and pushes to the package registry.
See `scripts/release.sh` for details.

## Getting help

- Open an issue on the [GitLab project](https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced)
- Check [architecture.md](architecture.md) for design decisions
- Check [operations.md](operations.md) for operational questions
