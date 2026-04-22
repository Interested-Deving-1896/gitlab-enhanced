#!/usr/bin/env bash
# Install the Incus executor scripts to /usr/local/lib/gitlab-runner-incus/.
# Run this on the runner host after cloning the repository.
#
# Usage:
#   sudo bash runtime/incus/runner/install.sh

set -euo pipefail

DEST="/usr/local/lib/gitlab-runner-incus"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Installing Incus executor scripts to ${DEST}"
mkdir -p "${DEST}"

for script in config.sh prepare.sh run.sh cleanup.sh; do
  install -m 0755 "${SCRIPT_DIR}/${script}" "${DEST}/${script}"
  echo "  installed ${script}"
done

echo "Done. Scripts installed to ${DEST}"
echo "Next: copy runtime/incus/runner/config.toml to /etc/gitlab-runner/config.toml"
echo "      and fill in the runner token."
