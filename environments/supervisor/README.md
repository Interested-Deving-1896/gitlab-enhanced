# Workspace Supervisor

The supervisor binary is responsible for bootstrapping a workspace container:
it reads `devcontainer.json` or `.gitpod.yml`, installs declared tools, and
launches the configured IDE process.

## Source

This is a git subtree of the Gitpod Classic supervisor:
https://github.com/gitpod-io/gitpod (path: `components/supervisor`)

To pull the subtree:

```bash
git subtree add \
  --prefix environments/supervisor \
  https://github.com/gitpod-io/gitpod.git main \
  --squash
```

## Role in gitlab-enhanced

`abstraction/environment/incus.go` calls the supervisor binary inside workspace
containers when `spec.IDE` is not `openvscode-server`. The binary is expected at
`/usr/local/bin/supervisor` inside the container image.

The workspace Dockerfile (`runtime/incus/images/workspace/Dockerfile`) should
build and install the supervisor binary during the image build. Until the subtree
is pulled, only `openvscode-server` IDE mode is available.

## Build

Once the subtree is pulled:

```bash
cd environments/supervisor
go build -o /usr/local/bin/supervisor ./cmd/supervisor/
```

## Configuration

The supervisor reads workspace configuration in priority order:
1. `.devcontainer/devcontainer.json`
2. `.gitpod.yml`
3. Environment variables (`GITPOD_TASKS`, `GITPOD_PORTS`)

## IDE support

| IDE value         | Supervisor flag         | Binary required              |
|-------------------|-------------------------|------------------------------|
| `openvscode-server` | (not used — direct)   | `/usr/local/bin/openvscode-server` |
| `jetbrains-idea`  | `--ide intellij`        | JetBrains Gateway            |
| `jetbrains-goland`| `--ide goland`          | JetBrains Gateway            |
| `theia`           | `--ide theia`           | Theia server                 |
