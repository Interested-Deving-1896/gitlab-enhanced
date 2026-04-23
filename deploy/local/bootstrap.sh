#!/usr/bin/env bash
# Bootstrap a fresh machine for gitlab-enhanced.
#
# What this script does:
#   1. Installs system dependencies (Incus, Go, Ansible, rudolfs, soft-serve)
#   2. Initialises Incus (bridge network + storage pool)
#   3. Applies Incus profiles from runtime/incus/profiles/
#   4. Scaffolds config/local.yaml if it doesn't exist
#   5. Builds the gitlab-enhanced CLI binary
#   6. Prints next steps
#
# Usage:
#   curl -fsSL https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced/-/raw/main/deploy/local/bootstrap.sh | bash
#   # or, from a local clone:
#   bash deploy/local/bootstrap.sh
#
# Supported: Ubuntu 22.04+, Debian 12+
# Requires:  sudo, curl, git

set -euo pipefail

# ── Colours ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[1;34m'; RESET='\033[0m'

info()  { echo -e "${BLUE}▶${RESET}  $*"; }
ok()    { echo -e "  ${GREEN}✓${RESET}  $*"; }
warn()  { echo -e "  ${YELLOW}⚠${RESET}  $*"; }
fail()  { echo -e "  ${RED}✗${RESET}  $*" >&2; exit 1; }

# ── Locate repo root ──────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

info "Repository root: $REPO_ROOT"

# ── OS check ─────────────────────────────────────────────────────────────────
if [[ "$(uname -s)" != "Linux" ]]; then
  fail "This script only supports Linux. For macOS, use a remote Incus host."
fi

# shellcheck source=/dev/null
. /etc/os-release
if [[ "$ID" != "ubuntu" && "$ID" != "debian" ]]; then
  warn "Detected OS: $PRETTY_NAME — only Ubuntu/Debian are tested. Proceeding anyway."
fi

# ── Versions ─────────────────────────────────────────────────────────────────
GO_VERSION="${GO_VERSION:-1.24.2}"
BUILDKIT_VERSION="${BUILDKIT_VERSION:-v0.19.0}"
SOFT_SERVE_VERSION="${SOFT_SERVE_VERSION:-v0.7.4}"
RUDOLFS_VERSION="${RUDOLFS_VERSION:-0.3.7}"

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  GOARCH="amd64" ;;
  aarch64) GOARCH="arm64" ;;
  *)       fail "Unsupported architecture: $ARCH" ;;
esac

# ── Helper: check if a binary exists ─────────────────────────────────────────
have() { command -v "$1" &>/dev/null; }

# ── 1. System packages ────────────────────────────────────────────────────────
info "Installing system packages"
sudo apt-get update -qq
sudo apt-get install -y -qq \
  curl git ca-certificates gnupg lsb-release \
  build-essential pkg-config \
  python3 python3-pip python3-venv \
  ansible \
  jq \
  2>/dev/null
ok "System packages installed"

# ── 2. Go ─────────────────────────────────────────────────────────────────────
if have go && [[ "$(go version 2>/dev/null | awk '{print $3}')" == "go${GO_VERSION}" ]]; then
  ok "Go ${GO_VERSION} already installed"
else
  info "Installing Go ${GO_VERSION}"
  GOTARBALL="go${GO_VERSION}.linux-${GOARCH}.tar.gz"
  curl -fsSL "https://go.dev/dl/${GOTARBALL}" -o "/tmp/${GOTARBALL}"
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf "/tmp/${GOTARBALL}"
  rm "/tmp/${GOTARBALL}"
  # Add to PATH for this session
  export PATH="/usr/local/go/bin:$PATH"
  ok "Go ${GO_VERSION} installed"
fi

# Ensure Go bin dir is in PATH
export PATH="/usr/local/go/bin:${HOME}/go/bin:$PATH"

# ── 3. Incus ──────────────────────────────────────────────────────────────────
if have incus; then
  ok "Incus already installed ($(incus version 2>/dev/null | head -1 || echo 'unknown version'))"
else
  info "Installing Incus via Zabbly repository"
  # Zabbly provides up-to-date Incus packages for Ubuntu/Debian
  sudo mkdir -p /etc/apt/keyrings
  curl -fsSL https://pkgs.zabbly.com/key.asc \
    | sudo gpg --dearmor -o /etc/apt/keyrings/zabbly.gpg
  CODENAME="$(lsb_release -cs)"
  echo "deb [signed-by=/etc/apt/keyrings/zabbly.gpg] https://pkgs.zabbly.com/incus/stable ${CODENAME} main" \
    | sudo tee /etc/apt/sources.list.d/zabbly-incus-stable.list >/dev/null
  sudo apt-get update -qq
  sudo apt-get install -y -qq incus
  ok "Incus installed"
fi

# Add current user to the incus-admin group if not already a member
if ! groups | grep -qw incus-admin; then
  info "Adding $USER to incus-admin group"
  sudo usermod -aG incus-admin "$USER"
  warn "Group membership change requires a new login session."
  warn "Run 'newgrp incus-admin' or log out and back in, then re-run this script."
fi

# ── 4. Initialise Incus ───────────────────────────────────────────────────────
info "Initialising Incus"

# Use preseed to avoid interactive prompts
if ! incus network show gitlab-enhanced &>/dev/null; then
  incus admin init --preseed <<'PRESEED'
config: {}
networks:
- config:
    ipv4.address: 10.200.0.1/24
    ipv4.nat: "true"
    ipv6.address: none
  description: "gitlab-enhanced bridge network"
  name: gitlab-enhanced
  type: bridge
storage_pools:
- config:
    size: 50GB
  description: ""
  driver: btrfs
  name: default
profiles:
- config: {}
  description: ""
  devices:
    eth0:
      name: eth0
      nictype: bridged
      parent: gitlab-enhanced
      type: nic
    root:
      path: /
      pool: default
      type: disk
  name: default
projects: []
cluster: null
PRESEED
  ok "Incus initialised"
else
  ok "Incus already initialised (network gitlab-enhanced exists)"
fi

# ── 5. Apply Incus profiles ───────────────────────────────────────────────────
info "Applying Incus profiles"
PROFILES_DIR="$REPO_ROOT/runtime/incus/profiles"

for profile_file in "$PROFILES_DIR"/*.yaml; do
  profile_name="$(basename "$profile_file" .yaml)"
  if incus profile show "$profile_name" &>/dev/null; then
    incus profile edit "$profile_name" < "$profile_file"
    ok "Updated profile: $profile_name"
  else
    incus profile create "$profile_name" < "$profile_file"
    ok "Created profile: $profile_name"
  fi
done

# ── 6. soft-serve ─────────────────────────────────────────────────────────────
if have soft; then
  ok "soft-serve already installed"
else
  info "Installing soft-serve ${SOFT_SERVE_VERSION}"
  SOFT_TARBALL="soft-serve_${SOFT_SERVE_VERSION#v}_linux_${GOARCH}.tar.gz"
  curl -fsSL "https://github.com/charmbracelet/soft-serve/releases/download/${SOFT_SERVE_VERSION}/${SOFT_TARBALL}" \
    -o "/tmp/${SOFT_TARBALL}"
  sudo tar -C /usr/local/bin -xzf "/tmp/${SOFT_TARBALL}" soft
  rm "/tmp/${SOFT_TARBALL}"
  ok "soft-serve installed"
fi

# ── 7. rudolfs (LFS server) ───────────────────────────────────────────────────
if have rudolfs; then
  ok "rudolfs already installed"
else
  info "Building rudolfs ${RUDOLFS_VERSION} from source"
  if ! have cargo; then
    info "Installing Rust toolchain"
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --no-modify-path
    export PATH="${HOME}/.cargo/bin:$PATH"
  fi
  cargo install rudolfs --version "$RUDOLFS_VERSION" --locked
  ok "rudolfs installed"
fi

# ── 8. Scaffold config/local.yaml ────────────────────────────────────────────
LOCAL_YAML="$REPO_ROOT/config/local.yaml"
if [[ -f "$LOCAL_YAML" ]]; then
  ok "config/local.yaml already exists — skipping"
else
  info "Creating config/local.yaml from example"
  cp "$REPO_ROOT/config/local.yaml.example" "$LOCAL_YAML"
  # Set storage and LFS paths to sensible local defaults
  DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/gitlab-enhanced"
  mkdir -p "$DATA_DIR/data" "$DATA_DIR/lfs"
  sed -i "s|/data/gitlab-enhanced|${DATA_DIR}|g" "$LOCAL_YAML"
  ok "config/local.yaml created (data dir: $DATA_DIR)"
fi

# ── 9. Build the CLI ──────────────────────────────────────────────────────────
info "Building gitlab-enhanced CLI"
cd "$REPO_ROOT"
go mod tidy -e 2>/dev/null || true
go build -o bin/gitlab-enhanced ./cmd/gitlab-enhanced/
ok "Binary: $REPO_ROOT/bin/gitlab-enhanced"

# Suggest adding to PATH
if [[ ":$PATH:" != *":$REPO_ROOT/bin:"* ]]; then
  warn "Add the binary to your PATH:"
  warn "  echo 'export PATH=\"$REPO_ROOT/bin:\$PATH\"' >> ~/.bashrc && source ~/.bashrc"
fi

# ── 10. Next steps ────────────────────────────────────────────────────────────
echo
echo -e "${GREEN}Bootstrap complete.${RESET}"
echo
echo "Next steps:"
echo "  1. Review and edit config/local.yaml for your machine"
echo "  2. Run:  gitlab-enhanced init"
echo "  3. Run:  gitlab-enhanced up"
echo "  4. Run:  gitlab-enhanced status"
echo
echo "Documentation: $REPO_ROOT/README.md"
