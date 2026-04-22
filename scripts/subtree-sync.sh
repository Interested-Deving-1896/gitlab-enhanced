#!/usr/bin/env bash
# Sync all subtrees from their upstreams.
# Usage:
#   ./scripts/subtree-sync.sh              # sync all
#   ./scripts/subtree-sync.sh core/omnibus # sync one

set -euo pipefail

SUBTREES_FILE="$(git rev-parse --show-toplevel)/.subtrees"

sync_subtree() {
  local path="$1" remote="$2" branch="$3"
  echo "→ Syncing $path from $remote ($branch)"
  git subtree pull --prefix="$path" "$remote" "$branch" --squash -m "chore: sync $path from upstream"
}

if [[ $# -eq 1 ]]; then
  # Sync a specific subtree
  target="$1"
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^#.*$ || -z "$line" ]] && continue
    read -r path remote branch <<< "$line"
    if [[ "$path" == "$target" ]]; then
      sync_subtree "$path" "$remote" "$branch"
      exit 0
    fi
  done < "$SUBTREES_FILE"
  echo "Error: subtree '$target' not found in .subtrees" >&2
  exit 1
else
  # Sync all subtrees
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^#.*$ || -z "$line" ]] && continue
    read -r path remote branch <<< "$line"
    sync_subtree "$path" "$remote" "$branch"
  done < "$SUBTREES_FILE"
fi

echo "✅ Subtree sync complete"
