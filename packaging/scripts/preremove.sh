#!/usr/bin/env bash
# Pre-remove script for gitlab-enhanced package.
# Runs before package files are removed. Does NOT delete user data.
set -euo pipefail

# Stop any running gitlab-enhanced managed services gracefully
if command -v gitlab-enhanced &>/dev/null; then
  gitlab-enhanced down --force 2>/dev/null || true
fi

# Remove PATH hint
rm -f /etc/profile.d/gitlab-enhanced.sh

echo "gitlab-enhanced removed. Data preserved at /var/lib/gitlab-enhanced."
echo "To fully clean up: rm -rf /var/lib/gitlab-enhanced /etc/gitlab-enhanced"
