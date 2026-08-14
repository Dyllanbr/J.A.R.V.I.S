#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
scanner="$repository_root/scripts/check-secrets.sh"
git_bin="${GIT:-git}"
test_root="$(mktemp -d -t jarvis-secret-test.XXXXXX)"

cleanup() {
  rm -rf "$test_root"
}
trap cleanup EXIT

"$git_bin" -C "$test_root" init --quiet
printf 'foundation scanner fixture\n' >"$test_root/clean.txt"
GIT="$git_bin" bash "$scanner" "$test_root" >/dev/null

synthetic_github="github_pat_""SYNTHETIC_ONLY_1234567890_ABCDEFGHIJ"
synthetic_jwt="eyJ""syntheticHeader.eyJ""syntheticPayload.syntheticSignature"
synthetic_npm="npm_""SYNTHETICONLY1234567890ABCDEFGHIJ"
synthetic_meta="EAA""SYNTHETICONLY1234567890ABCDEFGHIJKLMN"
synthetic_private="-----BEGIN ""ENCRYPTED PRIVATE KEY-----"
synthetic_aws="AKIA""SYNTHETICONLY123"
synthetic_google="AIza""SYNTHETICONLY1234567890ABCDEFGHIJ"
synthetic_slack="xoxb-""SYNTHETIC-ONLY-1234567890"
synthetic_openai="sk-""SYNTHETICONLY1234567890"

printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n' \
  "$synthetic_github" \
  "$synthetic_jwt" \
  "_authToken=$synthetic_npm" \
  "$synthetic_meta" \
  "$synthetic_private" \
  "$synthetic_aws" \
  "$synthetic_google" \
  "$synthetic_slack" \
  "$synthetic_openai" >"$test_root/secrets.txt"

set +e
output="$(GIT="$git_bin" bash "$scanner" "$test_root" 2>&1)"
status=$?
set -e

if [[ "$status" -eq 0 ]]; then
  echo "Secret scanner negative test failed: prohibited patterns were accepted." >&2
  exit 1
fi

for synthetic_value in \
  "$synthetic_github" "$synthetic_jwt" "$synthetic_npm" "$synthetic_meta" \
  "$synthetic_private" "$synthetic_aws" "$synthetic_google" "$synthetic_slack" \
  "$synthetic_openai"; do
  if [[ "$output" == *"$synthetic_value"* ]]; then
    echo "Secret scanner negative test failed: a synthetic value was exposed." >&2
    exit 1
  fi
done

for expected_type in \
  "private key" "GitHub token" "npm token" "npm auth token" "JWT" \
  "Meta token" "AWS access key" "Google API key" "Slack token" \
  "OpenAI API key"; do
  if [[ "$output" != *"$expected_type"* ]]; then
    printf 'Secret scanner negative test failed: missing detection for %s.\n' \
      "$expected_type" >&2
    exit 1
  fi
done

if [[ "$output" != *"secrets.txt:1"* ]]; then
  echo "Secret scanner negative test failed: file and line were not reported." >&2
  exit 1
fi

echo "Secret scanner self-test passed without exposing synthetic values."
