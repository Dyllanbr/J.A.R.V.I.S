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

income_description="${JARVIS_IOS_E2E_DESCRIPTION}_income"

counts="$(
  docker compose \
    --project-name "$JARVIS_INTEGRATION_COMPOSE_PROJECT_NAME" \
    --file "$JARVIS_INTEGRATION_COMPOSE_FILE" \
    exec -T postgres \
    psql --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1 \
    --set=owner_id="$JARVIS_INTEGRATION_OWNER_ID" \
    --set=description="$JARVIS_IOS_E2E_DESCRIPTION" \
    --set=income_description="$income_description" \
    --username "$JARVIS_POSTGRES_USER" \
    --dbname "$JARVIS_POSTGRES_DB" <<'SQL'
WITH expense_target AS (
    SELECT id
    FROM transactions
    WHERE user_id = :'owner_id'
      AND description = :'description'
      AND type = 'EXPENSE'
      AND amount_minor = 4250
      AND currency = 'BRL'
      AND payment_method = 'PIX'
      AND category_id = 'expense.food'
      AND origin = 'IOS'
      AND status = 'RECORDED'
),
income_target AS (
    SELECT id
    FROM transactions
    WHERE user_id = :'owner_id'
      AND description = :'income_description'
      AND type = 'INCOME'
      AND amount_minor = 4250
      AND currency = 'BRL'
      AND payment_method IS NULL
      AND category_id = 'income.salary'
      AND origin = 'IOS'
      AND status = 'RECORDED'
),
expense_transaction_count AS (
    SELECT count(*) AS value FROM expense_target
),
expense_audit_count AS (
    SELECT count(*) AS value
    FROM audit_events
    WHERE user_id = :'owner_id'
      AND aggregate_id IN (SELECT id FROM expense_target)
      AND aggregate_type = 'EXPENSE'
      AND event_type = 'EXPENSE_RECORDED'
),
expense_idempotency_count AS (
    SELECT count(*) AS value
    FROM idempotency_records
    WHERE user_id = :'owner_id'
      AND transaction_id IN (SELECT id FROM expense_target)
      AND operation = 'CREATE_EXPENSE'
      AND state = 'COMPLETED'
),
income_transaction_count AS (
    SELECT count(*) AS value FROM income_target
),
income_audit_count AS (
    SELECT count(*) AS value
    FROM audit_events
    WHERE user_id = :'owner_id'
      AND aggregate_id IN (SELECT id FROM income_target)
      AND aggregate_type = 'INCOME'
      AND event_type = 'INCOME_RECORDED'
),
income_idempotency_count AS (
    SELECT count(*) AS value
    FROM idempotency_records
    WHERE user_id = :'owner_id'
      AND transaction_id IN (SELECT id FROM income_target)
      AND operation = 'CREATE_INCOME'
      AND state = 'COMPLETED'
)
SELECT expense_transaction_count.value
    || '|' || expense_audit_count.value
    || '|' || expense_idempotency_count.value
    || '|' || income_transaction_count.value
    || '|' || income_audit_count.value
    || '|' || income_idempotency_count.value
FROM expense_transaction_count,
     expense_audit_count,
     expense_idempotency_count,
     income_transaction_count,
     income_audit_count,
     income_idempotency_count;
SQL
)"

if [[ "$counts" != "1|1|1|1|1|1" ]]; then
  echo "iOS real integration postcondition failed (Expense transaction|audit|idempotency|Income transaction|audit|idempotency=$counts)." >&2
  exit 1
fi

echo "iOS real integration PostgreSQL postcondition passed for categorized Expense and Income (1 transaction, 1 audit event, 1 idempotency record each)."
