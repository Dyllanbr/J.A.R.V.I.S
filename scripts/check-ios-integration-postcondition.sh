#!/usr/bin/env bash

set -euo pipefail

for variable_name in \
  JARVIS_INTEGRATION_COMPOSE_FILE \
  JARVIS_INTEGRATION_COMPOSE_PROJECT_NAME \
  JARVIS_INTEGRATION_OWNER_ID \
  JARVIS_IOS_E2E_DESCRIPTION \
  JARVIS_POSTGRES_DB \
  JARVIS_POSTGRES_USER; do
  if [[ -z "${!variable_name:-}" ]]; then
    echo "iOS integration postcondition requires $variable_name." >&2
    exit 1
  fi
done

counts="$(
  docker compose \
    --project-name "$JARVIS_INTEGRATION_COMPOSE_PROJECT_NAME" \
    --file "$JARVIS_INTEGRATION_COMPOSE_FILE" \
    exec -T postgres \
    psql --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1 \
    --set=owner_id="$JARVIS_INTEGRATION_OWNER_ID" \
    --set=description="$JARVIS_IOS_E2E_DESCRIPTION" \
    --username "$JARVIS_POSTGRES_USER" \
    --dbname "$JARVIS_POSTGRES_DB" <<'SQL'
WITH target AS (
    SELECT id
    FROM transactions
    WHERE user_id = :'owner_id'
      AND description = :'description'
      AND type = 'EXPENSE'
      AND amount_minor = 4250
      AND currency = 'BRL'
      AND payment_method = 'PIX'
      AND origin = 'IOS'
      AND status = 'RECORDED'
),
transaction_count AS (
    SELECT count(*) AS value FROM target
),
audit_count AS (
    SELECT count(*) AS value
    FROM audit_events
    WHERE user_id = :'owner_id'
      AND aggregate_id IN (SELECT id FROM target)
      AND aggregate_type = 'EXPENSE'
      AND event_type = 'EXPENSE_RECORDED'
),
idempotency_count AS (
    SELECT count(*) AS value
    FROM idempotency_records
    WHERE user_id = :'owner_id'
      AND transaction_id IN (SELECT id FROM target)
      AND operation = 'CREATE_EXPENSE'
      AND state = 'COMPLETED'
)
SELECT transaction_count.value || '|' || audit_count.value || '|' || idempotency_count.value
FROM transaction_count, audit_count, idempotency_count;
SQL
)"

if [[ "$counts" != "1|1|1" ]]; then
  echo "iOS real integration postcondition failed (transactions|audit_events|idempotency_records=$counts)." >&2
  exit 1
fi

echo "iOS real integration PostgreSQL postcondition passed (1 transaction, 1 audit event, 1 idempotency record)."
