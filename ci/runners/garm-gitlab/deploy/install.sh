#!/usr/bin/env bash
# install.sh — build and install garm-gitlab on the local machine.
#
# Requires: Go 1.22+, Incus installed and running, systemd.
# Run as root (or with sudo).
#
# Usage:
#   sudo ./deploy/install.sh [--config-only]
#
# --config-only  Skip build/install; only write the example config and
#                create the system user. Useful when the binary is already
#                installed and you just want to (re)configure.

set -euo pipefail

BINARY_NAME="garm-gitlab"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/garm-gitlab"
DATA_DIR="/var/lib/garm-gitlab"
EXECUTOR_DIR="/opt/garm-gitlab/executor"
SERVICE_FILE="/etc/systemd/system/garm-gitlab.service"
SERVICE_USER="garm-gitlab"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

CONFIG_ONLY=false
for arg in "$@"; do
    case "${arg}" in
        --config-only) CONFIG_ONLY=true ;;
        *) echo "Unknown argument: ${arg}" >&2; exit 1 ;;
    esac
done

# ── Helpers ──────────────────────────────────────────────────────────────────

info()  { echo "[INFO]  $*"; }
warn()  { echo "[WARN]  $*" >&2; }
error() { echo "[ERROR] $*" >&2; exit 1; }

require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        error "This script must be run as root (or with sudo)."
    fi
}

# ── Build ─────────────────────────────────────────────────────────────────────

build_binary() {
    info "Building ${BINARY_NAME}..."
    cd "${REPO_ROOT}"

    if ! command -v go &>/dev/null; then
        error "Go is not installed. Install Go 1.22+ and retry."
    fi

    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    REQUIRED="1.22"
    if [ "$(printf '%s\n' "${REQUIRED}" "${GO_VERSION}" | sort -V | head -n1)" != "${REQUIRED}" ]; then
        error "Go ${REQUIRED}+ required, found ${GO_VERSION}"
    fi

    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
        -o "${REPO_ROOT}/${BINARY_NAME}" \
        ./cmd/garm-gitlab/

    info "Build complete: ${REPO_ROOT}/${BINARY_NAME}"
}

# ── Install ───────────────────────────────────────────────────────────────────

install_binary() {
    info "Installing binary to ${INSTALL_DIR}/${BINARY_NAME}"
    install -m 0755 "${REPO_ROOT}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
}

install_executor_scripts() {
    info "Installing executor scripts to ${EXECUTOR_DIR}"
    mkdir -p "${EXECUTOR_DIR}"
    install -m 0755 "${REPO_ROOT}/executor/base.sh"    "${EXECUTOR_DIR}/base.sh"
    install -m 0755 "${REPO_ROOT}/executor/prepare.sh" "${EXECUTOR_DIR}/prepare.sh"
    install -m 0755 "${REPO_ROOT}/executor/run.sh"     "${EXECUTOR_DIR}/run.sh"
    install -m 0755 "${REPO_ROOT}/executor/cleanup.sh" "${EXECUTOR_DIR}/cleanup.sh"
}

create_user() {
    if id "${SERVICE_USER}" &>/dev/null; then
        info "User ${SERVICE_USER} already exists"
    else
        info "Creating system user ${SERVICE_USER}"
        useradd --system --no-create-home --shell /usr/sbin/nologin \
            --comment "garm-gitlab runner manager" "${SERVICE_USER}"
    fi

    # Add to the incus group so the service can reach the Incus socket.
    if getent group incus &>/dev/null; then
        usermod -aG incus "${SERVICE_USER}"
        info "Added ${SERVICE_USER} to the incus group"
    else
        warn "incus group not found — add ${SERVICE_USER} to the Incus socket group manually"
    fi
}

create_directories() {
    info "Creating directories"
    mkdir -p "${CONFIG_DIR}" "${DATA_DIR}"
    chown "${SERVICE_USER}:${SERVICE_USER}" "${DATA_DIR}"
    chmod 0750 "${DATA_DIR}"
}

write_example_config() {
    local dest="${CONFIG_DIR}/config.toml.example"
    if [ -f "${dest}" ]; then
        info "Example config already exists at ${dest} — skipping"
        return
    fi

    info "Writing example config to ${dest}"
    cat > "${dest}" << 'EOF'
# garm-gitlab configuration — copy to config.toml and fill in your values.

[api]
listen_address = "0.0.0.0:8080"
# webhook_secret must match the secret set on the GitLab webhook.
webhook_secret = "change-me"

[gitlab]
url   = "https://gitlab.com"
token = "glpat-XXXXXXXXXXXXXXXXXXXX"

# Standard pool — Ubuntu Noble containers, no privilege escalation.
[[pool]]
id                 = "ubuntu-noble"
registration_token = "GR1348941XXXXXXXXXXXX"
tags               = ["linux", "incus"]
run_untagged       = false
image              = "ubuntu:noble"
incus_profile      = "default"
privileged         = false
min_idle           = 1
max_runners        = 5
idle_timeout       = "10m"

# Privileged pool for live-build (Debian ISO building).
[[pool]]
id                 = "live-build"
registration_token = "GR1348941YYYYYYYYYYYY"
tags               = ["linux", "incus", "live-build", "privileged"]
run_untagged       = false
image              = "debian:bookworm"
incus_profile      = "default"
privileged         = true
min_idle           = 0
max_runners        = 2
idle_timeout       = "5m"

[pool.extra_config]
"limits.cpu"    = "4"
"limits.memory" = "8GB"
EOF
}

install_systemd_unit() {
    info "Installing systemd unit to ${SERVICE_FILE}"
    install -m 0644 "${SCRIPT_DIR}/garm-gitlab.service" "${SERVICE_FILE}"
    systemctl daemon-reload
}

enable_service() {
    info "Enabling and starting garm-gitlab.service"
    systemctl enable --now garm-gitlab.service
}

# ── Main ──────────────────────────────────────────────────────────────────────

require_root

if [ "${CONFIG_ONLY}" = "false" ]; then
    build_binary
    install_binary
    install_executor_scripts
fi

create_user
create_directories
write_example_config
install_systemd_unit

if [ -f "${CONFIG_DIR}/config.toml" ]; then
    enable_service
    info "garm-gitlab installed and started."
    info "Check status: systemctl status garm-gitlab"
else
    warn "No config.toml found at ${CONFIG_DIR}/config.toml"
    warn "Copy ${CONFIG_DIR}/config.toml.example to ${CONFIG_DIR}/config.toml,"
    warn "fill in your values, then run: systemctl enable --now garm-gitlab"
fi
