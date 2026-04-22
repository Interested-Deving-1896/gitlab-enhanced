#!/usr/bin/env bash
# Runs every time the devcontainer starts.
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"

echo "→ Ensuring config/local.yaml exists"
if [[ ! -f "$ROOT/config/local.yaml" ]]; then
  cp "$ROOT/config/local.yaml.example" "$ROOT/config/local.yaml"
  echo "  Created config/local.yaml from example — review and customise as needed"
fi

echo "→ Verifying Go module"
cd "$ROOT" && go mod tidy 2>/dev/null || true

echo "✅ Environment ready"
echo ""
echo "  Docs:    docs/"
echo "  Config:  config/local.yaml"
echo "  Scripts: scripts/"
echo ""
echo "  First time? Run: ./scripts/subtree-add-all.sh"
