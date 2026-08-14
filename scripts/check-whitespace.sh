#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
git_bin="${GIT:-git}"

cd "$repository_root"
if ! "$git_bin" diff --check >/dev/null; then
  echo "Whitespace diff check failed; inspect locally with git diff --check." >&2
  exit 1
fi

set +e
matches="$($git_bin grep --untracked --exclude-standard -nI -e '[[:blank:]]$' -- .)"
status=$?
set -e

case "$status" in
  0)
    echo "Trailing whitespace found:" >&2
    while IFS= read -r match; do
      file="${match%%:*}"
      remainder="${match#*:}"
      line="${remainder%%:*}"
      printf '  %s:%s (content redacted)\n' "$file" "$line" >&2
    done <<<"$matches"
    exit 1
    ;;
  1)
    echo "Whitespace checks passed."
    ;;
  *)
    echo "Whitespace checks could not be completed." >&2
    exit "$status"
    ;;
esac
