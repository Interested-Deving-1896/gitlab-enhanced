#!/usr/bin/env bash
# Add all subtrees for the first time (run once after cloning).
# Skips any subtree whose path already has content.
# Usage: ./scripts/subtree-add-all.sh

set -euo pipefail

SUBTREES_FILE="$(git rev-parse --show-toplevel)/.subtrees"
ROOT="$(git rev-parse --show-toplevel)"

while IFS= read -r line || [[ -n "$line" ]]; do
  [[ "$line" =~ ^#.*$ || -z "$line" ]] && continue
  read -r path remote branch <<< "$line"

  if [[ -n "$(ls -A "$ROOT/$path" 2>/dev/null)" ]]; then
    echo "⏭  Skipping $path (already populated)"
    continue
  fi

  echo "→ Adding subtree $path from $remote ($branch)"
  git subtree add --prefix="$path" "$remote" "$branch" --squash \
    -m "chore: add $path subtree from upstream" || {
    echo "⚠️  Failed to add $path — skipping (remote may be unavailable)"
  }
done < "$SUBTREES_FILE"

echo "✅ Subtree add complete"
