#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
backend_binary="${JARVIS_SMOKE_BINARY:-$repository_root/backend/bin/jarvis-api}"
smoke_host="${JARVIS_SMOKE_HOST:-127.0.0.1}"
smoke_port="${JARVIS_SMOKE_PORT:-18080}"
base_url="http://${smoke_host}:${smoke_port}"
npm_bin="${NPM:-npm}"
backend_pid=""
backend_log="$(mktemp -t jarvis-smoke.XXXXXX)"

print_backend_log() {
  echo "Backend log:" >&2
  if [[ -s "$backend_log" ]]; then
    sed 's/^/  /' "$backend_log" >&2
  else
    echo "  (empty)" >&2
  fi
}

cleanup() {
  original_status=$?
  trap - EXIT INT TERM
  cleanup_status=0

  if [[ -n "$backend_pid" ]]; then
    if kill -0 "$backend_pid" 2>/dev/null; then
      if ! kill -TERM "$backend_pid" 2>/dev/null; then
        cleanup_status=1
      fi

      for _ in {1..50}; do
        if ! kill -0 "$backend_pid" 2>/dev/null; then
          break
        fi
        sleep 0.1
      done

      if kill -0 "$backend_pid" 2>/dev/null; then
        echo "Graceful shutdown timed out; using SIGKILL fallback." >&2
        kill -KILL "$backend_pid" 2>/dev/null || true
        cleanup_status=1
      fi
    fi

    set +e
    wait "$backend_pid"
    wait_status=$?
    set -e

    if [[ "$wait_status" -ne 0 ]]; then
      echo "Backend exited with status $wait_status during smoke cleanup." >&2
      cleanup_status=1
    fi
  fi

  if [[ "$original_status" -ne 0 || "$cleanup_status" -ne 0 ]]; then
    print_backend_log
  fi

  rm -f "$backend_log"

  if [[ "$original_status" -ne 0 ]]; then
    exit "$original_status"
  fi
  exit "$cleanup_status"
}
trap cleanup EXIT INT TERM

if [[ ! -x "$backend_binary" ]]; then
  echo "Smoke test backend binary is missing or not executable: $backend_binary" >&2
  exit 1
fi

node "$repository_root/scripts/check-port.mjs" "$smoke_host" "$smoke_port"

JARVIS_HTTP_ADDRESS="${smoke_host}:${smoke_port}" \
JARVIS_SHUTDOWN_TIMEOUT="5s" \
JARVIS_FINANCIAL_API_ENABLED="false" \
  "$backend_binary" >"$backend_log" 2>&1 &
backend_pid=$!

ready=0
for _ in {1..50}; do
  if ! kill -0 "$backend_pid" 2>/dev/null; then
    echo "Backend exited before readiness." >&2
    exit 1
  fi

  if curl --fail --silent --show-error \
    --connect-timeout 1 \
    --max-time 2 \
    "$base_url/healthz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.1
done

if [[ "$ready" -ne 1 ]]; then
  echo "Backend readiness timed out." >&2
  exit 1
fi

if ! (
  cd "$repository_root/qa/playwright"
  unset NO_COLOR
  JARVIS_API_BASE_URL="$base_url" "$npm_bin" test
); then
  echo "Playwright smoke test failed." >&2
  exit 1
fi

echo "Smoke test passed and graceful shutdown will be validated."
