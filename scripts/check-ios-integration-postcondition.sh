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
card_name="${JARVIS_IOS_E2E_DESCRIPTION}_card"
purchase_card_name="${JARVIS_IOS_E2E_DESCRIPTION}_purchase_card"
purchase_one_time_description="${JARVIS_IOS_E2E_DESCRIPTION}_purchase_one_time"
purchase_installment_description="${JARVIS_IOS_E2E_DESCRIPTION}_purchase_installment"

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
    --set=card_name="$card_name" \
    --set=purchase_card_name="$purchase_card_name" \
    --set=purchase_one_time_description="$purchase_one_time_description" \
    --set=purchase_installment_description="$purchase_installment_description" \
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
),
card_target AS (
    SELECT id
    FROM credit_cards
    WHERE user_id = :'owner_id'
      AND name = :'card_name'
      AND last_four = '4821'
      AND brand = 'VISA'
      AND closing_day = 1
      AND due_day = 10
      AND credit_limit_minor = 250000
      AND credit_limit_currency = 'BRL'
      AND status = 'ARCHIVED'
      AND archived_at IS NOT NULL
),
card_count AS (
    SELECT count(*) AS value FROM card_target
),
card_created_audit_count AS (
    SELECT count(*) AS value FROM credit_card_audit_events
    WHERE user_id = :'owner_id'
      AND credit_card_id IN (SELECT id FROM card_target)
      AND event_type = 'CREDIT_CARD_CREATED'
),
card_archived_audit_count AS (
    SELECT count(*) AS value FROM credit_card_audit_events
    WHERE user_id = :'owner_id'
      AND credit_card_id IN (SELECT id FROM card_target)
      AND event_type = 'CREDIT_CARD_ARCHIVED'
),
card_create_idempotency_count AS (
    SELECT count(*) AS value FROM credit_card_idempotency_records
    WHERE user_id = :'owner_id'
      AND credit_card_id IN (SELECT id FROM card_target)
      AND operation = 'CREATE_CREDIT_CARD'
      AND state = 'COMPLETED'
),
card_archive_idempotency_count AS (
    SELECT count(*) AS value FROM credit_card_idempotency_records
    WHERE user_id = :'owner_id'
      AND credit_card_id IN (SELECT id FROM card_target)
      AND operation = 'ARCHIVE_CREDIT_CARD'
      AND state = 'COMPLETED'
),
card_legacy_transaction_count AS (
    SELECT count(*) AS value FROM transactions
    WHERE user_id = :'owner_id' AND description = :'card_name'
),
card_legacy_audit_count AS (
    SELECT count(*) AS value FROM audit_events
    WHERE user_id = :'owner_id' AND aggregate_id IN (SELECT id FROM card_target)
),
card_legacy_idempotency_count AS (
    SELECT count(*) AS value FROM idempotency_records
    WHERE user_id = :'owner_id' AND transaction_id IN (SELECT id FROM card_target)
),
purchase_card_target AS (
    SELECT id
    FROM credit_cards
    WHERE user_id = :'owner_id'
      AND name = :'purchase_card_name'
      AND last_four = '4821'
      AND brand = 'VISA'
      AND closing_day = 1
      AND due_day = 10
      AND status = 'ACTIVE'
),
purchase_one_time_expense_target AS (
    SELECT id
    FROM transactions
    WHERE user_id = :'owner_id'
      AND description = :'purchase_one_time_description'
      AND type = 'EXPENSE'
      AND amount_minor = 8000
      AND currency = 'BRL'
      AND payment_method = 'CREDIT'
      AND category_id IS NULL
      AND credit_card_id IN (SELECT id FROM purchase_card_target)
      AND origin = 'IOS'
      AND status = 'RECORDED'
),
purchase_installment_expense_target AS (
    SELECT id
    FROM transactions
    WHERE user_id = :'owner_id'
      AND description = :'purchase_installment_description'
      AND type = 'EXPENSE'
      AND amount_minor = 12000
      AND currency = 'BRL'
      AND payment_method = 'CREDIT'
      AND category_id IS NULL
      AND credit_card_id IN (SELECT id FROM purchase_card_target)
      AND origin = 'IOS'
      AND status = 'RECORDED'
),
purchase_plan_target AS (
    SELECT id
    FROM installment_plans
    WHERE user_id = :'owner_id'
      AND expense_id IN (SELECT id FROM purchase_installment_expense_target)
      AND credit_card_id IN (SELECT id FROM purchase_card_target)
      AND total_minor = 12000
      AND total_currency = 'BRL'
      AND installment_count = 2
      AND status = 'CANCELLED'
      AND cancelled_on IS NOT NULL
),
purchase_one_time_expense_count AS (
    SELECT count(*) AS value FROM purchase_one_time_expense_target
),
purchase_one_time_expense_audit_count AS (
    SELECT count(*) AS value
    FROM audit_events
    WHERE user_id = :'owner_id'
      AND aggregate_id IN (SELECT id FROM purchase_one_time_expense_target)
      AND aggregate_type = 'EXPENSE'
      AND event_type = 'EXPENSE_RECORDED'
),
purchase_one_time_create_idempotency_count AS (
    SELECT count(*) AS value
    FROM card_purchase_idempotency_records
    WHERE user_id = :'owner_id'
      AND operation = 'CREATE_CARD_PURCHASE'
      AND state = 'COMPLETED'
      AND expense_id IN (SELECT id FROM purchase_one_time_expense_target)
),
purchase_installment_expense_count AS (
    SELECT count(*) AS value FROM purchase_installment_expense_target
),
purchase_installment_expense_audit_count AS (
    SELECT count(*) AS value
    FROM audit_events
    WHERE user_id = :'owner_id'
      AND aggregate_id IN (SELECT id FROM purchase_installment_expense_target)
      AND aggregate_type = 'EXPENSE'
      AND event_type = 'EXPENSE_RECORDED'
),
purchase_installment_create_idempotency_count AS (
    SELECT count(*) AS value
    FROM card_purchase_idempotency_records
    WHERE user_id = :'owner_id'
      AND operation = 'CREATE_CARD_PURCHASE'
      AND state = 'COMPLETED'
      AND expense_id IN (SELECT id FROM purchase_installment_expense_target)
),
purchase_plan_count AS (
    SELECT count(*) AS value FROM purchase_plan_target
),
purchase_one_time_plan_count AS (
    SELECT count(*) AS value
    FROM installment_plans
    WHERE user_id = :'owner_id'
      AND expense_id IN (SELECT id FROM purchase_one_time_expense_target)
),
purchase_plan_created_audit_count AS (
    SELECT count(*) AS value
    FROM installment_plan_audit_events
    WHERE user_id = :'owner_id'
      AND installment_plan_id IN (SELECT id FROM purchase_plan_target)
      AND event_type = 'INSTALLMENT_PLAN_CREATED'
),
purchase_plan_cancelled_audit_count AS (
    SELECT count(*) AS value
    FROM installment_plan_audit_events
    WHERE user_id = :'owner_id'
      AND installment_plan_id IN (SELECT id FROM purchase_plan_target)
      AND event_type = 'INSTALLMENT_PLAN_CANCELLED'
),
purchase_cancel_idempotency_count AS (
    SELECT count(*) AS value
    FROM installment_plan_idempotency_records
    WHERE user_id = :'owner_id'
      AND plan_id IN (SELECT id FROM purchase_plan_target)
      AND operation = 'CANCEL_INSTALLMENT_PLAN'
      AND state = 'COMPLETED'
),
purchase_legacy_idempotency_count AS (
    SELECT count(*) AS value
    FROM idempotency_records
    WHERE user_id = :'owner_id'
      AND transaction_id IN (
          SELECT id FROM purchase_one_time_expense_target
          UNION ALL
          SELECT id FROM purchase_installment_expense_target
      )
),
purchase_unexpected_expense_count AS (
    SELECT count(*) AS value
    FROM transactions
    WHERE user_id = :'owner_id'
      AND credit_card_id IN (SELECT id FROM purchase_card_target)
      AND type = 'EXPENSE'
      AND id NOT IN (
          SELECT id FROM purchase_one_time_expense_target
          UNION ALL
          SELECT id FROM purchase_installment_expense_target
      )
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
    || '|' || card_count.value
    || '|' || card_created_audit_count.value
    || '|' || card_archived_audit_count.value
    || '|' || card_create_idempotency_count.value
    || '|' || card_archive_idempotency_count.value
    || '|' || card_legacy_transaction_count.value
    || '|' || card_legacy_audit_count.value
    || '|' || card_legacy_idempotency_count.value
    || '|' || purchase_one_time_expense_count.value
    || '|' || purchase_one_time_expense_audit_count.value
    || '|' || purchase_one_time_create_idempotency_count.value
    || '|' || purchase_installment_expense_count.value
    || '|' || purchase_installment_expense_audit_count.value
    || '|' || purchase_installment_create_idempotency_count.value
    || '|' || purchase_plan_count.value
    || '|' || purchase_one_time_plan_count.value
    || '|' || purchase_plan_created_audit_count.value
    || '|' || purchase_plan_cancelled_audit_count.value
    || '|' || purchase_cancel_idempotency_count.value
    || '|' || purchase_legacy_idempotency_count.value
    || '|' || purchase_unexpected_expense_count.value
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
     suggestion_suppression_count,
     card_count,
     card_created_audit_count,
     card_archived_audit_count,
     card_create_idempotency_count,
     card_archive_idempotency_count,
     card_legacy_transaction_count,
     card_legacy_audit_count,
     card_legacy_idempotency_count,
     purchase_one_time_expense_count,
     purchase_one_time_expense_audit_count,
     purchase_one_time_create_idempotency_count,
     purchase_installment_expense_count,
     purchase_installment_expense_audit_count,
     purchase_installment_create_idempotency_count,
     purchase_plan_count,
     purchase_one_time_plan_count,
     purchase_plan_created_audit_count,
     purchase_plan_cancelled_audit_count,
     purchase_cancel_idempotency_count,
     purchase_legacy_idempotency_count,
     purchase_unexpected_expense_count;
SQL
)"

if [[ "$counts" != "1|1|1|1|1|1|1|1|1|1|1|0|0|0|3|3|3|1|1|1|0|1|1|1|1|1|0|0|0|1|1|1|1|1|1|1|0|1|1|1|0|0" ]]; then
  echo "iOS real integration postcondition failed (legacy flows, suggestion flow, CreditCard, then CardPurchase one-time expense|audit|completion, installment expense|audit|completion, plan|one-time plan|created audit|cancelled audit|cancel completion|legacy idempotency|unexpected expense=$counts)." >&2
  exit 1
fi

echo "iOS real integration PostgreSQL postcondition passed for legacy flows, CreditCard, and CardPurchase/InstallmentPlan exactly once with zero legacy financial side effects."
