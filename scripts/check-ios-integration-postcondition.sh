#!/usr/bin/env bash

set -euo pipefail

for variable_name in \
  JARVIS_INTEGRATION_COMPOSE_FILE \
  JARVIS_INTEGRATION_COMPOSE_PROJECT_NAME \
  JARVIS_INTEGRATION_OWNER_ID \
  JARVIS_IOS_E2E_DESCRIPTION \
  JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION \
  JARVIS_IOS_E2E_SUGGESTION_STARTS_ON \
  JARVIS_POSTGRES_DB \
  JARVIS_POSTGRES_USER; do
  if [[ -z "${!variable_name:-}" ]]; then
    echo "iOS integration postcondition requires $variable_name." >&2
    exit 1
  fi
done

income_description="${JARVIS_IOS_E2E_DESCRIPTION}_income"
recurrence_description="${JARVIS_IOS_E2E_DESCRIPTION}_recurrence"

counts="$(
  docker compose \
    --project-name "$JARVIS_INTEGRATION_COMPOSE_PROJECT_NAME" \
    --file "$JARVIS_INTEGRATION_COMPOSE_FILE" \
    exec -T postgres \
    psql --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1 \
    --set=owner_id="$JARVIS_INTEGRATION_OWNER_ID" \
    --set=description="$JARVIS_IOS_E2E_DESCRIPTION" \
    --set=income_description="$income_description" \
    --set=recurrence_description="$recurrence_description" \
    --set=suggestion_description="$JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION" \
    --set=suggestion_starts_on="$JARVIS_IOS_E2E_SUGGESTION_STARTS_ON" \
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
recurrence_target AS (
    SELECT id
    FROM recurrences
    WHERE user_id = :'owner_id'
      AND description = :'recurrence_description'
      AND transaction_type = 'EXPENSE'
      AND expected_amount_minor = 4250
      AND currency = 'BRL'
      AND frequency = 'MONTHLY'
      AND status = 'CANCELLED'
      AND cancelled_at IS NOT NULL
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
),
recurrence_count AS (
    SELECT count(*) AS value FROM recurrence_target
),
recurrence_created_audit_count AS (
    SELECT count(*) AS value
    FROM recurrence_audit_events
    WHERE user_id = :'owner_id'
      AND recurrence_id IN (SELECT id FROM recurrence_target)
      AND event_type = 'RECURRENCE_CREATED'
),
recurrence_cancelled_audit_count AS (
    SELECT count(*) AS value
    FROM recurrence_audit_events
    WHERE user_id = :'owner_id'
      AND recurrence_id IN (SELECT id FROM recurrence_target)
      AND event_type = 'RECURRENCE_CANCELLED'
),
recurrence_create_idempotency_count AS (
    SELECT count(*) AS value
    FROM recurrence_idempotency_records
    WHERE user_id = :'owner_id'
      AND recurrence_id IN (SELECT id FROM recurrence_target)
      AND operation = 'CREATE_RECURRENCE'
      AND state = 'COMPLETED'
),
recurrence_cancel_idempotency_count AS (
    SELECT count(*) AS value
    FROM recurrence_idempotency_records
    WHERE user_id = :'owner_id'
      AND recurrence_id IN (SELECT id FROM recurrence_target)
      AND operation = 'CANCEL_RECURRENCE'
      AND state = 'COMPLETED'
),
recurrence_transaction_side_effect_count AS (
    SELECT count(*) AS value
    FROM transactions
    WHERE user_id = :'owner_id'
      AND description = :'recurrence_description'
),
recurrence_transaction_audit_side_effect_count AS (
    SELECT count(*) AS value
    FROM audit_events
    WHERE user_id = :'owner_id'
      AND aggregate_id IN (SELECT id FROM recurrence_target)
),
recurrence_transaction_idempotency_side_effect_count AS (
    SELECT count(*) AS value
    FROM idempotency_records
    WHERE user_id = :'owner_id'
      AND transaction_id IN (SELECT id FROM recurrence_target)
),
suggestion_evidence_target AS (
    SELECT id
    FROM transactions
    WHERE user_id = :'owner_id'
      AND description = :'suggestion_description'
      AND type = 'EXPENSE'
      AND amount_minor = 6390
      AND currency = 'BRL'
      AND payment_method = 'PIX'
      AND category_id = 'expense.subscriptions'
      AND origin = 'IOS'
      AND status = 'RECORDED'
),
suggestion_evidence_count AS (
    SELECT count(*) AS value FROM suggestion_evidence_target
),
suggestion_evidence_audit_count AS (
    SELECT count(*) AS value
    FROM audit_events
    WHERE user_id = :'owner_id'
      AND aggregate_id IN (SELECT id FROM suggestion_evidence_target)
      AND aggregate_type = 'EXPENSE'
      AND event_type = 'EXPENSE_RECORDED'
),
suggestion_evidence_idempotency_count AS (
    SELECT count(*) AS value
    FROM idempotency_records
    WHERE user_id = :'owner_id'
      AND transaction_id IN (SELECT id FROM suggestion_evidence_target)
      AND operation = 'CREATE_EXPENSE'
      AND state = 'COMPLETED'
),
suggestion_recurrence_target AS (
    SELECT id
    FROM recurrences
    WHERE user_id = :'owner_id'
      AND description = :'suggestion_description'
      AND transaction_type = 'EXPENSE'
      AND expected_amount_minor = 6390
      AND currency = 'BRL'
      AND frequency = 'MONTHLY'
      AND starts_on = :'suggestion_starts_on'::date
      AND status = 'ACTIVE'
      AND cancelled_at IS NULL
),
suggestion_recurrence_count AS (
    SELECT count(*) AS value FROM suggestion_recurrence_target
),
suggestion_recurrence_audit_count AS (
    SELECT count(*) AS value
    FROM recurrence_audit_events
    WHERE user_id = :'owner_id'
      AND recurrence_id IN (SELECT id FROM suggestion_recurrence_target)
      AND event_type = 'RECURRENCE_CREATED'
),
suggestion_recurrence_idempotency_count AS (
    SELECT count(*) AS value
    FROM recurrence_idempotency_records
    WHERE user_id = :'owner_id'
      AND recurrence_id IN (SELECT id FROM suggestion_recurrence_target)
      AND operation = 'CREATE_RECURRENCE'
      AND state = 'COMPLETED'
),
suggestion_suppression_count AS (
    SELECT count(*) AS value
    FROM recurrence_suggestion_suppressions
    WHERE user_id = :'owner_id'
      AND suggestion_id LIKE 'rsg_%'
)
SELECT expense_transaction_count.value
    || '|' || expense_audit_count.value
    || '|' || expense_idempotency_count.value
    || '|' || income_transaction_count.value
    || '|' || income_audit_count.value
    || '|' || income_idempotency_count.value
    || '|' || recurrence_count.value
    || '|' || recurrence_created_audit_count.value
    || '|' || recurrence_cancelled_audit_count.value
    || '|' || recurrence_create_idempotency_count.value
    || '|' || recurrence_cancel_idempotency_count.value
    || '|' || recurrence_transaction_side_effect_count.value
    || '|' || recurrence_transaction_audit_side_effect_count.value
    || '|' || recurrence_transaction_idempotency_side_effect_count.value
    || '|' || suggestion_evidence_count.value
    || '|' || suggestion_evidence_audit_count.value
    || '|' || suggestion_evidence_idempotency_count.value
    || '|' || suggestion_recurrence_count.value
    || '|' || suggestion_recurrence_audit_count.value
    || '|' || suggestion_recurrence_idempotency_count.value
    || '|' || suggestion_suppression_count.value
FROM expense_transaction_count,
     expense_audit_count,
     expense_idempotency_count,
     income_transaction_count,
     income_audit_count,
     income_idempotency_count,
     recurrence_count,
     recurrence_created_audit_count,
     recurrence_cancelled_audit_count,
     recurrence_create_idempotency_count,
     recurrence_cancel_idempotency_count,
     recurrence_transaction_side_effect_count,
     recurrence_transaction_audit_side_effect_count,
     recurrence_transaction_idempotency_side_effect_count,
     suggestion_evidence_count,
     suggestion_evidence_audit_count,
     suggestion_evidence_idempotency_count,
     suggestion_recurrence_count,
     suggestion_recurrence_audit_count,
     suggestion_recurrence_idempotency_count,
     suggestion_suppression_count;
SQL
)"

if [[ "$counts" != "1|1|1|1|1|1|1|1|1|1|1|0|0|0|3|3|3|1|1|1|0" ]]; then
  echo "iOS real integration postcondition failed (legacy Expense/Income/Recurrence counts followed by suggestion evidence transaction|audit|idempotency, confirmed recurrence|audit|idempotency, suppression=$counts)." >&2
  exit 1
fi

echo "iOS real integration PostgreSQL postcondition passed for Expense/Income, manual Recurrence, and suggestion Review/Confirm with exactly three evidence Expenses and one confirmed Recurrence."
