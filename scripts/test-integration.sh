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
default_integration_owner="usr_test_api_owner_001"
api_owner="${JARVIS_INTEGRATION_OWNER_ID:-$default_integration_owner}"
owner_b="${JARVIS_INTEGRATION_OWNER_B_ID:-usr_test_api_owner_002}"
owner_isolation_mode="${JARVIS_INTEGRATION_OWNER_ISOLATION:-false}"

validate_owner_id() {
  local owner_id="$1"
  if [[ ! "$owner_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]]; then
    echo "Integration owner must contain 1-128 ASCII letters, digits, '.', '_' or '-'." >&2
    return 1
  fi
}

case "$owner_isolation_mode" in
  "" | false | FALSE | 0)
    owner_isolation_mode=false
    ;;
  true | TRUE | 1)
    owner_isolation_mode=true
    ;;
  *)
    echo "JARVIS_INTEGRATION_OWNER_ISOLATION must be true or false." >&2
    exit 2
    ;;
esac

validate_owner_id "$api_owner"
if [[ "$owner_isolation_mode" == true ]]; then
  validate_owner_id "$owner_b"
  if [[ "$api_owner" == "$owner_b" ]]; then
    echo "Owner isolation requires distinct owner IDs." >&2
    exit 2
  fi
  if ((${#client_command[@]} == 0)); then
    echo "Owner isolation requires a real client command." >&2
    exit 2
  fi
fi
export JARVIS_INTEGRATION_OWNER_ID="$api_owner"

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

start_api() {
  local owner="$1"
  printf '\n=== Financial API owner %s ===\n' "$owner" >>"$api_log"
  JARVIS_HTTP_ADDRESS="${api_host}:${api_port}" \
  JARVIS_SHUTDOWN_TIMEOUT="5s" \
  JARVIS_FINANCIAL_API_ENABLED="true" \
  JARVIS_OWNER_ID="$owner" \
    "$api_binary" >>"$api_log" 2>&1 &
  api_pid=$!

  ready=0
  for _ in {1..50}; do
    if ! kill -0 "$api_pid" 2>/dev/null; then
      echo "Financial API exited before readiness." >&2
      return 1
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
    return 1
  fi
}

psql_test() {
  docker compose \
    --project-name "$project_name" \
    --file "$compose_file" \
    exec -T postgres \
    psql --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1 \
    --username "$JARVIS_POSTGRES_USER" \
    --dbname "$JARVIS_POSTGRES_DB" \
    "$@"
}

seed_user() {
  local owner="$1"
  psql_test --set=owner_id="$owner" <<'SQL'
INSERT INTO users (id, created_at, updated_at)
VALUES (:'owner_id', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO NOTHING;
SQL
}

owner_counts() {
  local owner="$1"
  psql_test --set=owner_id="$owner" <<'SQL'
SELECT (SELECT count(*) FROM transactions WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM audit_events WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM idempotency_records WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM recurrences WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM recurrence_audit_events WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM recurrence_idempotency_records WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM recurrence_suggestion_suppressions WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM credit_cards WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM credit_card_audit_events WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM credit_card_idempotency_records WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM installment_plans WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM installment_plan_audit_events WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM installment_plan_idempotency_records WHERE user_id = :'owner_id') || '|' ||
       (SELECT count(*) FROM card_purchase_idempotency_records WHERE user_id = :'owner_id');
SQL
}

read_owner_ids() {
  local owner="$1"
  local table_name="$2"
  case "$table_name" in
    credit_cards)
      psql_test --set=owner_id="$owner" <<'SQL'
SELECT id FROM credit_cards WHERE user_id = :'owner_id' ORDER BY id;
SQL
      ;;
    recurrences)
      psql_test --set=owner_id="$owner" <<'SQL'
SELECT id FROM recurrences WHERE user_id = :'owner_id' ORDER BY id;
SQL
      ;;
    installment_plans)
      psql_test --set=owner_id="$owner" <<'SQL'
SELECT id FROM installment_plans WHERE user_id = :'owner_id' ORDER BY id;
SQL
      ;;
    *) echo "Unsupported owner-isolation table: $table_name" >&2; return 2 ;;
  esac
}

response_counter=0
request_response_file=""
request_status=""

capture_get() {
  local url="$1"
  response_counter=$((response_counter + 1))
  request_response_file="$temporary_dir/owner-isolation-response-${response_counter}.json"
  if ! request_status="$(curl --silent --show-error --output "$request_response_file" --write-out '%{http_code}' --connect-timeout 2 --max-time 5 -H 'Accept: application/json' "$url")"; then
    echo "Owner isolation request failed: $url" >&2
    return 1
  fi
}

assert_list_count() {
  local url="$1"
  local expected_count="$2"
  capture_get "$url"
  if [[ "$request_status" != 200 ]]; then
    echo "Owner isolation list $url returned HTTP $request_status." >&2
    return 1
  fi
  node - "$request_response_file" "$expected_count" <<'NODE'
const fs = require('fs');
const file = process.argv[2];
const expected = Number(process.argv[3]);
const body = JSON.parse(fs.readFileSync(file, 'utf8'));
if (!body || !Array.isArray(body.items) || body.items.length !== expected) {
  process.exit(1);
}
NODE
}

assert_error_code() {
  local url="$1"
  local expected_status="$2"
  local expected_code="$3"
  capture_get "$url"
  if [[ "$request_status" != "$expected_status" ]]; then
    echo "Owner isolation request $url returned HTTP $request_status, want $expected_status." >&2
    return 1
  fi
  node - "$request_response_file" "$expected_code" <<'NODE'
const fs = require('fs');
const file = process.argv[2];
const expected = process.argv[3];
const body = JSON.parse(fs.readFileSync(file, 'utf8'));
if (!body || !body.error || body.error.code !== expected) {
  process.exit(1);
}
NODE
}

assert_owner_isolation() {
  local owner_a="$1"
  local owner_b_id="$2"
  local owner_a_counts="$3"
  local owner_a_transaction_month="$4"
  local owner_a_transaction_count="$5"

  local expected_cards expected_recurrences expected_plans
  IFS='|' read -r _ _ _ expected_recurrences _ _ _ expected_cards _ _ expected_plans _ _ _ <<<"$owner_a_counts"

  local -a card_ids=()
  local -a plan_ids=()
  local card_ids_raw plan_ids_raw
  local id
  if ! card_ids_raw="$(read_owner_ids "$owner_a" credit_cards)"; then
    echo "Owner isolation could not read owner A credit-card IDs." >&2
    return 1
  fi
  if ! plan_ids_raw="$(read_owner_ids "$owner_a" installment_plans)"; then
    echo "Owner isolation could not read owner A installment-plan IDs." >&2
    return 1
  fi
  while IFS= read -r id; do
    [[ -n "$id" ]] && card_ids+=("$id")
  done <<<"$card_ids_raw"
  while IFS= read -r id; do
    [[ -n "$id" ]] && plan_ids+=("$id")
  done <<<"$plan_ids_raw"

  assert_list_count "$api_base_url/v1/cards" 0
  assert_list_count "$api_base_url/v1/recurrences" 0
  assert_list_count "$api_base_url/v1/installment-plans" 0
  assert_list_count "$api_base_url/v1/recurrence-suggestions" 0
  if [[ -n "$owner_a_transaction_month" ]]; then
    assert_list_count "$api_base_url/v1/transactions?month=$owner_a_transaction_month" 0
  fi

  if ((${#card_ids[@]} > 0)); then
    for id in "${card_ids[@]}"; do
      assert_error_code "$api_base_url/v1/cards/$id" 404 CREDIT_CARD_NOT_FOUND
    done
  fi
  if ((${#plan_ids[@]} > 0)); then
    for id in "${plan_ids[@]}"; do
      assert_error_code "$api_base_url/v1/installment-plans/$id" 404 INSTALLMENT_PLAN_NOT_FOUND
    done
  fi

  local owner_b_counts
  owner_b_counts="$(owner_counts "$owner_b_id")"
  if [[ "$owner_b_counts" != "0|0|0|0|0|0|0|0|0|0|0|0|0|0" ]]; then
    echo "Owner isolation SQL postcondition failed for $owner_b_id: $owner_b_counts" >&2
    return 1
  fi

  stop_api
  start_api "$owner_a"
  assert_list_count "$api_base_url/v1/cards" "$expected_cards"
  assert_list_count "$api_base_url/v1/recurrences" "$expected_recurrences"
  assert_list_count "$api_base_url/v1/installment-plans" "$expected_plans"
  if [[ -n "$owner_a_transaction_month" ]]; then
    assert_list_count "$api_base_url/v1/transactions?month=$owner_a_transaction_month" "$owner_a_transaction_count"
  fi
  local owner_a_after_counts
  owner_a_after_counts="$(owner_counts "$owner_a")"
  if [[ "$owner_a_after_counts" != "$owner_a_counts" ]]; then
    echo "Owner A SQL postcondition changed after owner switch: before=$owner_a_counts after=$owner_a_after_counts" >&2
    return 1
  fi
  if ((${#card_ids[@]} > 0)); then
    for id in "${card_ids[@]}"; do
      capture_get "$api_base_url/v1/cards/$id"
      [[ "$request_status" == 200 ]] || return 1
    done
  fi
  if ((${#plan_ids[@]} > 0)); then
    for id in "${plan_ids[@]}"; do
      capture_get "$api_base_url/v1/installment-plans/$id"
      [[ "$request_status" == 200 ]] || return 1
    done
  fi
  echo "Owner isolation passed: owner A $owner_a remained intact while owner B $owner_b_id saw no owner A data or details."
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
seed_user "$api_owner"
if [[ "$owner_isolation_mode" == true ]]; then
  seed_user "$owner_b"
fi
start_api "$api_owner"

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

if [[ "$owner_isolation_mode" == true ]]; then
  owner_a_counts="$(owner_counts "$api_owner")"
  owner_a_transaction_month="$(psql_test --set=owner_id="$api_owner" <<'SQL' | tr -d '[:space:]'
SELECT COALESCE(to_char(max(occurred_at AT TIME ZONE 'America/Sao_Paulo'), 'YYYY-MM'), '')
FROM transactions
WHERE user_id = :'owner_id';
SQL
)"
  owner_a_transaction_count="0"
  if [[ -n "$owner_a_transaction_month" ]]; then
    owner_a_transaction_count="$(psql_test \
      --set=owner_id="$api_owner" \
      --set=transaction_month="$owner_a_transaction_month" <<'SQL' | tr -d '[:space:]'
SELECT count(*)
FROM transactions
WHERE user_id = :'owner_id'
  AND to_char(occurred_at AT TIME ZONE 'America/Sao_Paulo', 'YYYY-MM') = :'transaction_month';
SQL
)"
  fi

  stop_api
  start_api "$owner_b"
  assert_owner_isolation \
    "$api_owner" \
    "$owner_b" \
    "$owner_a_counts" \
    "$owner_a_transaction_month" \
    "$owner_a_transaction_count"
fi

echo "PostgreSQL and financial API integration tests passed; cleanup will be validated."
