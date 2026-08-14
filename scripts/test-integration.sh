#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="$repository_root/compose.yaml"
project_name="jarvis-it-$$-$RANDOM"
go_bin="${GO:-go}"
npm_bin="${NPM:-npm}"
client_command=("$@")
api_host="127.0.0.1"
api_port="${JARVIS_FINANCIAL_API_TEST_PORT:-18081}"
api_base_url="http://${api_host}:${api_port}"
api_owner="usr_test_api_owner_001"
temporary_dir="$(mktemp -d -t jarvis-financial-api.XXXXXX)"
api_binary="$temporary_dir/jarvis-api"
migrate_binary="$temporary_dir/jarvis-migrate"
api_log="$temporary_dir/jarvis-api.log"
api_pid=""
cleanup_required=false

export JARVIS_POSTGRES_DB="jarvis_test"
export JARVIS_POSTGRES_USER="jarvis_test"
export JARVIS_POSTGRES_PASSWORD="jarvis_synthetic_$$_$RANDOM"
export JARVIS_POSTGRES_PORT="0"

print_api_log() {
  echo "Financial API log:" >&2
  if [[ -s "$api_log" ]]; then
    sed 's/^/  /' "$api_log" >&2
  else
    echo "  (empty)" >&2
  fi
}

stop_api() {
  api_cleanup_status=0
  if [[ -z "$api_pid" ]]; then
    return 0
  fi

  if kill -0 "$api_pid" 2>/dev/null; then
    kill -TERM "$api_pid" 2>/dev/null || api_cleanup_status=1
    for _ in {1..50}; do
      if ! kill -0 "$api_pid" 2>/dev/null; then
        break
      fi
      sleep 0.1
    done
    if kill -0 "$api_pid" 2>/dev/null; then
      echo "Financial API graceful shutdown timed out; using SIGKILL fallback." >&2
      kill -KILL "$api_pid" 2>/dev/null || true
      api_cleanup_status=1
    fi
  fi

  set +e
  wait "$api_pid"
  wait_status=$?
  set -e
  api_pid=""
  if [[ "$wait_status" -ne 0 ]]; then
    echo "Financial API exited with status $wait_status." >&2
    api_cleanup_status=1
  fi
  return "$api_cleanup_status"
}

cleanup() {
  status=$?
  trap - EXIT INT TERM

  if ! stop_api; then
    if ((status == 0)); then
      status=1
    fi
  fi
  if ((status != 0)); then
    print_api_log
  fi

  if [[ "$cleanup_required" == true ]]; then
    if ((status != 0)); then
      docker compose --project-name "$project_name" --file "$compose_file" logs --no-color postgres >&2 || true
    fi

    if ! docker compose --project-name "$project_name" --file "$compose_file" down --timeout 10 --volumes --remove-orphans >/dev/null; then
      echo "PostgreSQL integration cleanup failed." >&2
      if ((status == 0)); then
        status=1
      fi
    fi
  fi

  rm -rf "$temporary_dir"
  exit "$status"
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  echo "PostgreSQL integration tests require a running Docker daemon." >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "PostgreSQL integration tests require Docker Compose." >&2
  exit 1
fi
if ! command -v curl >/dev/null 2>&1 || ! command -v node >/dev/null 2>&1; then
  echo "Financial API integration tests require curl and Node.js." >&2
  exit 1
fi

node "$repository_root/scripts/check-port.mjs" "$api_host" "$api_port"

cleanup_required=true
docker compose \
  --project-name "$project_name" \
  --file "$compose_file" \
  up --detach --wait --wait-timeout 45 postgres

published_address="$(docker compose \
  --project-name "$project_name" \
  --file "$compose_file" \
  port postgres 5432)"
published_port="${published_address##*:}"
if [[ ! "$published_port" =~ ^[0-9]+$ ]] || ((published_port < 1 || published_port > 65535)); then
  echo "Docker did not publish a valid PostgreSQL test port." >&2
  exit 1
fi

export JARVIS_TEST_DATABASE_URL="postgres://${JARVIS_POSTGRES_USER}:${JARVIS_POSTGRES_PASSWORD}@127.0.0.1:${published_port}/${JARVIS_POSTGRES_DB}?sslmode=disable"
export JARVIS_DATABASE_URL="$JARVIS_TEST_DATABASE_URL"

(
  cd "$repository_root/backend"
  "$go_bin" test -race -cover -count=1 -tags=integration ./internal/modules/transactions/adapters/postgres
  "$go_bin" build -trimpath -o "$api_binary" ./cmd/api
  "$go_bin" build -trimpath -o "$migrate_binary" ./cmd/migrate
)

"$migrate_binary" up
docker compose \
  --project-name "$project_name" \
  --file "$compose_file" \
  exec -T postgres \
  psql --quiet --set=ON_ERROR_STOP=1 \
  --username "$JARVIS_POSTGRES_USER" \
  --dbname "$JARVIS_POSTGRES_DB" \
  --command "INSERT INTO users (id, created_at, updated_at) VALUES ('usr_test_api_owner_001', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)"

JARVIS_HTTP_ADDRESS="${api_host}:${api_port}" \
JARVIS_SHUTDOWN_TIMEOUT="5s" \
JARVIS_FINANCIAL_API_ENABLED="true" \
JARVIS_OWNER_ID="$api_owner" \
  "$api_binary" >"$api_log" 2>&1 &
api_pid=$!

ready=0
for _ in {1..50}; do
  if ! kill -0 "$api_pid" 2>/dev/null; then
    echo "Financial API exited before readiness." >&2
    exit 1
  fi
  if curl --fail --silent --show-error \
    --connect-timeout 1 \
    --max-time 2 \
    "$api_base_url/healthz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.1
done
if [[ "$ready" -ne 1 ]]; then
  echo "Financial API readiness timed out." >&2
  exit 1
fi

if ((${#client_command[@]} > 0)); then
  JARVIS_API_BASE_URL="$api_base_url" \
  JARVIS_IOS_E2E_BASE_URL="$api_base_url" \
  JARVIS_INTEGRATION_COMPOSE_FILE="$compose_file" \
  JARVIS_INTEGRATION_COMPOSE_PROJECT_NAME="$project_name" \
  JARVIS_INTEGRATION_OWNER_ID="$api_owner" \
    "${client_command[@]}"
else
  (
    cd "$repository_root/qa/playwright"
    unset NO_COLOR
    JARVIS_API_BASE_URL="$api_base_url" \
    JARVIS_FINANCIAL_API_TESTS="true" \
      "$npm_bin" test
  )
fi

echo "PostgreSQL and financial API integration tests passed; cleanup will be validated."
