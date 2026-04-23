# Incus Cloud-Init Configs

Cloud-init user-data files applied to Incus VMs and containers on first boot.

## Files

| File             | Applied to          | Purpose                                      |
|------------------|---------------------|----------------------------------------------|
| `buildkit.yaml`  | BuildKit VM         | Installs and starts buildkitd                |
| `workspace.yaml` | Workspace containers| Installs dev tools, creates gitpod user      |

## Usage in Go code

`abstraction/build/incus.go` embeds `buildkit.yaml` as a Go constant
(`buildkitCloudInit`). To keep the source of truth here and avoid drift,
the constant should be kept in sync with this file manually, or replaced
with an `//go:embed` directive:

```go
//go:embed ../../../runtime/incus/cloud-init/buildkit.yaml
var buildkitCloudInit string
```

## Applying manually

To apply cloud-init to an existing instance (for testing):

```bash
# Write user-data to the instance
incus file push runtime/incus/cloud-init/buildkit.yaml buildkit/var/lib/cloud/seed/nocloud/user-data
incus exec buildkit -- cloud-init clean --reboot
```

## Validation

Validate cloud-init syntax before applying:

```bash
cloud-init schema --config-file runtime/incus/cloud-init/buildkit.yaml
```
