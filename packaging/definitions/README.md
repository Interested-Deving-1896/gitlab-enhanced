# Package Definitions

Distribution-specific package definitions for building native packages outside
of nfpm (which handles `.deb` and `.rpm`). This directory contains definitions
for package formats that require their own tooling.

## Planned definitions

### `alpine/APKBUILD`

Alpine Linux package definition for `apk add gitlab-enhanced`. Targets Alpine
3.19+ (the base for many container images).

```sh
# APKBUILD skeleton
pkgname=gitlab-enhanced
pkgver=0.1.0
pkgrel=0
pkgdesc="Local-first GitLab stack manager with Incus runtime"
url="https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced"
arch="x86_64 aarch64"
license="MIT"
depends="incus git git-lfs ansible"
makedepends="go"
source="$pkgname-$pkgver.tar.gz::https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced/-/archive/v$pkgver/$pkgname-v$pkgver.tar.gz"

build() {
    cd "$builddir"
    go build -o bin/gitlab-enhanced ./cmd/gitlab-enhanced/
}

package() {
    install -Dm755 "$builddir/bin/gitlab-enhanced" "$pkgdir/usr/local/bin/gitlab-enhanced"
    install -Dm644 "$builddir/config/defaults.yaml" "$pkgdir/etc/gitlab-enhanced/defaults.yaml"
}
```

### `homebrew/gitlab-enhanced.rb`

Homebrew formula for macOS (development use only — Incus is Linux-only, but the
CLI can be used to manage a remote Incus host):

```ruby
class GitlabEnhanced < Formula
  desc "Local-first GitLab stack manager with Incus runtime"
  homepage "https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced"
  url "https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced/-/archive/v0.1.0/gitlab-enhanced-v0.1.0.tar.gz"
  license "MIT"

  depends_on "go" => :build
  depends_on "ansible"

  def install
    system "go", "build", "-o", bin/"gitlab-enhanced", "./cmd/gitlab-enhanced/"
    etc.install "config/defaults.yaml" => "gitlab-enhanced/defaults.yaml"
  end
end
```

### `arch/PKGBUILD`

Arch Linux package for the AUR:

```sh
pkgname=gitlab-enhanced
pkgver=0.1.0
pkgrel=1
pkgdesc="Local-first GitLab stack manager with Incus runtime"
arch=('x86_64' 'aarch64')
url="https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced"
license=('MIT')
depends=('incus' 'git' 'git-lfs' 'ansible')
makedepends=('go')

build() {
    cd "$srcdir/$pkgname-$pkgver"
    go build -o bin/gitlab-enhanced ./cmd/gitlab-enhanced/
}

package() {
    cd "$srcdir/$pkgname-$pkgver"
    install -Dm755 bin/gitlab-enhanced "$pkgdir/usr/local/bin/gitlab-enhanced"
    install -Dm644 config/defaults.yaml "$pkgdir/etc/gitlab-enhanced/defaults.yaml"
}
```

## Status

All definitions above are templates. They will be fleshed out when the project
reaches a stable release version and a CI release pipeline is configured.
