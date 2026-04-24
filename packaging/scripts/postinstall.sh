#!/usr/bin/env bash
# Post-install script for gitlab-enhanced package.
# Runs after the package files are placed on disk.
set -euo pipefail

CONFIG_DIR="/etc/gitlab-enhanced"
DATA_DIR="/var/lib/gitlab-enhanced"
LOG_DIR="/var/log/gitlab-enhanced"

# Create runtime directories
install -d -m 0755 "${CONFIG_DIR}"
install -d -m 0750 "${DATA_DIR}"
install -d -m 0755 "${LOG_DIR}"

# Create local config from defaults if not present
if [[ ! -f "${CONFIG_DIR}/local.yaml" ]]; then
  cat > "${CONFIG_DIR}/local.yaml" <<'EOF'
# gitlab-enhanced local configuration
# Override any value from /etc/gitlab-enhanced/defaults.yaml here.
# See: gitlab-enhanced --help

lfs:
  path: /var/lib/gitlab-enhanced/lfs
EOF
  echo "Created ${CONFIG_DIR}/local.yaml"
fi

# Add gitlab-enhanced to PATH hint
if [[ ! -f /etc/profile.d/gitlab-enhanced.sh ]]; then
  cat > /etc/profile.d/gitlab-enhanced.sh <<'EOF'
# gitlab-enhanced
export PATH="/usr/local/bin:$PATH"
EOF
fi

# Reload systemd so new service units are recognized
if command -v systemctl &>/dev/null && systemctl is-system-running &>/dev/null; then
  systemctl daemon-reload
fi

echo "gitlab-enhanced installed. Run 'gitlab-enhanced init' to get started."
