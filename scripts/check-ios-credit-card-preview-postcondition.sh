#!/usr/bin/env bash

set -euo pipefail

for variable_name in JARVIS_INTEGRATION_COMPOSE_FILE JARVIS_INTEGRATION_COMPOSE_PROJECT_NAME JARVIS_INTEGRATION_OWNER_ID JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION JARVIS_POSTGRES_DB JARVIS_POSTGRES_USER; do
  if [[ -z "${!variable_name:-}" ]]; then
    echo "CreditCard Preview postcondition requires $variable_name." >&2
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
    --set=suggestion_description="$JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION" \
    --username "$JARVIS_POSTGRES_USER" \
    --dbname "$JARVIS_POSTGRES_DB" <<'SQL'
WITH suggestion_evidence AS (
    SELECT id
    FROM transactions
    WHERE user_id = :'owner_id'
      AND type = 'EXPENSE'
      AND description = :'suggestion_description'
      AND amount_minor = 6390
      AND currency = 'BRL'
      AND category_id = 'expense.subscriptions'
)
SELECT (SELECT count(*) FROM credit_cards WHERE user_id = :'owner_id')
    || '|' || (SELECT count(*) FROM credit_card_audit_events WHERE user_id = :'owner_id')
    || '|' || (SELECT count(*) FROM credit_card_idempotency_records WHERE user_id = :'owner_id')
    || '|' || (SELECT count(*) FROM transactions WHERE user_id = :'owner_id')
    || '|' || (SELECT count(*) FROM audit_events WHERE user_id = :'owner_id')
    || '|' || (SELECT count(*) FROM idempotency_records WHERE user_id = :'owner_id')
    || '|' || (SELECT count(*) FROM recurrences WHERE user_id = :'owner_id')
    || '|' || (SELECT count(*) FROM recurrence_audit_events WHERE user_id = :'owner_id')
    || '|' || (SELECT count(*) FROM recurrence_idempotency_records WHERE user_id = :'owner_id')
    || '|' || (SELECT count(*) FROM recurrence_suggestion_suppressions WHERE user_id = :'owner_id')
    || '|' || (SELECT count(*) FROM suggestion_evidence)
    || '|' || (
        SELECT count(*)
        FROM audit_events
        WHERE user_id = :'owner_id'
          AND aggregate_id IN (SELECT id FROM suggestion_evidence)
          AND aggregate_type = 'EXPENSE'
          AND event_type = 'EXPENSE_RECORDED'
    )
    || '|' || (
        SELECT count(*)
        FROM idempotency_records
        WHERE user_id = :'owner_id'
          AND transaction_id IN (SELECT id FROM suggestion_evidence)
          AND operation = 'CREATE_EXPENSE'
          AND state = 'COMPLETED'
    );
SQL
)"

if [[ "$counts" != "0|0|0|3|3|3|0|0|0|0|3|3|3" ]]; then
  echo "CreditCard Preview wrote financial state before explicit confirmation: $counts" >&2
  exit 1
fi

echo "CreditCard Preview iOS E2E preserved zero-write state before Confirm; the only legacy rows are the three required RecurrenceSuggestion evidence records."
