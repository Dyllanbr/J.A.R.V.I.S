#!/usr/bin/env bash

set -euo pipefail

repository_root="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
git_bin="${GIT:-git}"
private_key_label="PRIVATE KEY"
private_key_pattern='-----BEGIN (RSA |EC |DSA |OPENSSH |ENCRYPTED )?'"$private_key_label"'-----|-----BEGIN PGP '"$private_key_label"' BLOCK-----'

pattern_names=(
  "private key"
  "GitHub token"
  "npm token"
  "npm auth token"
  "JWT"
  "Meta token"
  "AWS access key"
  "Google API key"
  "Slack token"
  "OpenAI API key"
)

patterns=(
  "$private_key_pattern"
  'gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}'
  'npm_[A-Za-z0-9]{20,}'
  '_authToken[[:space:]]*=[[:space:]]*(npm_[A-Za-z0-9]{20,}|[A-Za-z0-9_-]{36,})'
  'eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}'
  'EAA[A-Za-z0-9]{30,}'
  'AKIA[0-9A-Z]{16}'
  'AIza[0-9A-Za-z_-]{30,}'
  'xox[baprs]-[A-Za-z0-9-]{10,}'
  'sk-(proj-)?[A-Za-z0-9_-]{20,}'
)

cd "$repository_root"
found=0

for ((index = 0; index < ${#patterns[@]}; index++)); do
  set +e
  matches="$($git_bin grep --untracked --exclude-standard -nEI \
    -e "${patterns[$index]}" -- .)"
  status=$?
  set -e

  case "$status" in
    0)
      found=1
      while IFS= read -r match; do
        file="${match%%:*}"
        remainder="${match#*:}"
        line="${remainder%%:*}"
        printf 'Potential secret at %s:%s (%s); value redacted.\n' \
          "$file" "$line" "${pattern_names[$index]}" >&2
      done <<<"$matches"
      ;;
    1)
      ;;
    *)
      printf 'Secret scan could not be completed for pattern type %s.\n' \
        "${pattern_names[$index]}" >&2
      exit "$status"
      ;;
  esac
done

if [[ "$found" -ne 0 ]]; then
  exit 1
fi

echo "No high-confidence secret patterns found."
