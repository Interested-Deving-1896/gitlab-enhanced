# Packaging Config

Package metadata and build configuration for distributing the `gitlab-enhanced`
CLI binary as native OS packages (`.deb`, `.rpm`, `.apk`).

## Files

### `nfpm.yaml`

[nfpm](https://nfpm.goreleaser.com/) configuration for building `.deb` and `.rpm`
packages from the compiled binary:

```yaml
name: gitlab-enhanced
arch: amd64
platform: linux
version: "${VERSION}"
maintainer: "OpenOS Project <hello@openos.dev>"
description: "Local-first GitLab stack manager with Incus runtime"
homepage: "https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced"
license: MIT

contents:
  - src: bin/gitlab-enhanced
    dst: /usr/local/bin/gitlab-enhanced
    file_info:
      mode: 0755

  - src: config/defaults.yaml
    dst: /etc/gitlab-enhanced/defaults.yaml
    type: config|noreplace

  - src: runtime/incus/profiles/
    dst: /usr/share/gitlab-enhanced/profiles/
    type: dir

  - src: deploy/local/bootstrap.sh
    dst: /usr/share/gitlab-enhanced/bootstrap.sh
    file_info:
      mode: 0755

scripts:
  postinstall: packaging/scripts/postinstall.sh
  preremove: packaging/scripts/preremove.sh
```

## Building packages

```bash
# Install nfpm
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

# Build .deb
VERSION=$(git describe --tags --always) nfpm package \
  --config packaging/config/nfpm.yaml \
  --packager deb \
  --target dist/

# Build .rpm
VERSION=$(git describe --tags --always) nfpm package \
  --config packaging/config/nfpm.yaml \
  --packager rpm \
  --target dist/
```

## GoReleaser integration

The `.goreleaser.yaml` at the repository root (when created) will reference
this config for automated release builds triggered by git tags.
