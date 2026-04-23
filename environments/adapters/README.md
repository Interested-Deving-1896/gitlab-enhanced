# Environment Adapters

Protocol adapters that translate between the gitlab-enhanced environment API and
external environment backends.

## Adapters

### `ona/`
Adapter for the Ona (Gitpod Flex) environment API. Translates `abstraction/environment`
`Manager` calls to Ona's REST API. Implemented in `abstraction/environment/ona.go`.

### `gitpod-classic/`
Adapter for Gitpod Classic running on Kubernetes. Implemented in
`abstraction/environment/gitpod_k8s.go`.

### `devcontainer/`
Adapter that reads `.devcontainer/devcontainer.json` and applies the declared
features, extensions, and lifecycle hooks inside an Incus container. Bridges
the devcontainer spec with the Incus runtime.

**Status**: Planned. Currently `incus.go` applies a fixed set of tools. A proper
devcontainer adapter would parse the JSON and apply features dynamically.

## Adding a new adapter

1. Implement the `Manager` interface from `abstraction/environment/interface.go`
2. Add a `FromConfig` constructor
3. Register it in `abstraction/environment/registry.go`
4. Add the backend name to `config/defaults.yaml` comments

## devcontainer spec reference

https://containers.dev/implementors/spec/
