#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
owner_isolation_mode=false
while (($# > 0)); do
  case "$1" in
    --owner-isolation)
      owner_isolation_mode=true
      ;;
    --real-api)
      ;;
    *)
      echo "Usage: scripts/test-ios-integration.sh [--real-api] [--owner-isolation]" >&2
      exit 2
      ;;
  esac
  shift
done
export JARVIS_IOS_E2E_DESCRIPTION="Mercado_sintetico_E2E_$$_$RANDOM"
export JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION="${JARVIS_IOS_E2E_DESCRIPTION}_suggestion"
if [[ "$owner_isolation_mode" == true ]]; then
  # The shared integration harness owns the database lifecycle.  Keep its
  # built-in source-list isolation disabled here so this wrapper can perform
  # the SafeAvailable A/B read on the very same database and API port.
  export JARVIS_INTEGRATION_OWNER_ISOLATION=false
  export JARVIS_INTEGRATION_OWNER_ID="${JARVIS_INTEGRATION_OWNER_ID:-usr_test_api_owner_001}"
  export JARVIS_INTEGRATION_OWNER_B_ID="${JARVIS_INTEGRATION_OWNER_B_ID:-usr_test_api_owner_002}"
  export JARVIS_IOS_OWNER_ISOLATION_MODE=true
  if [[ ! "$JARVIS_INTEGRATION_OWNER_B_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]]; then
    echo "JARVIS_INTEGRATION_OWNER_B_ID must contain 1-128 ASCII letters, digits, '.', '_' or '-'." >&2
    exit 2
  fi
  if [[ "$JARVIS_INTEGRATION_OWNER_ID" == "$JARVIS_INTEGRATION_OWNER_B_ID" ]]; then
    echo "Owner isolation requires distinct owner IDs." >&2
    exit 2
  fi
fi

inner_script="$(mktemp -t jarvis-ios-integration.XXXXXX)"
inner_cleanup() {
  status="$?"
  trap - EXIT INT TERM
  rm -f "$inner_script"
  exit "$status"
}
trap inner_cleanup EXIT INT TERM
cat >"$inner_script" <<'JARVIS_INNER'
    set -euo pipefail
    repository_root="$1"
    export JARVIS_IOS_E2E_SUGGESTION_STARTS_ON="$(node "$repository_root/scripts/prepare-ios-recurrence-suggestion-e2e.mjs")"
    bash "$repository_root/scripts/test-ios.sh" --card-preview-real-api
    bash "$repository_root/scripts/check-ios-credit-card-preview-postcondition.sh"
    real_api_temporary_dir="$(mktemp -d -t jarvis-real-api.XXXXXX)"
    real_api_cleanup() {
      status="$?"
      trap - EXIT INT TERM
      rm -rf "$real_api_temporary_dir"
      exit "$status"
    }
    trap real_api_cleanup EXIT INT TERM
    echo "Running legacy real-api UI flows serially (iPhone 15, iOS 17.5; no parallel workers)."
    xcodebuild test \
      -project "$repository_root/apps/ios/JARVIS.xcodeproj" \
      -scheme JARVIS \
      -destination "platform=iOS Simulator,name=iPhone 15,OS=17.5" \
      -derivedDataPath "$real_api_temporary_dir/DerivedData" \
      -resultBundlePath "$real_api_temporary_dir/LegacyRealAPI.xcresult" \
      -parallel-testing-enabled NO \
      -maximum-concurrent-test-simulator-destinations 1 \
      -enableCodeCoverage YES \
      CODE_SIGNING_ALLOWED=NO \
      JARVIS_IOS_TEST_MODE=real \
      "JARVIS_IOS_E2E_BASE_URL=$JARVIS_IOS_E2E_BASE_URL" \
      "JARVIS_IOS_E2E_DESCRIPTION=$JARVIS_IOS_E2E_DESCRIPTION" \
      "JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION=$JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION" \
      "JARVIS_IOS_E2E_SUGGESTION_STARTS_ON=$JARVIS_IOS_E2E_SUGGESTION_STARTS_ON" \
      -only-testing:JARVISUITests/JARVISUITests/testRegisterPreviewConfirmAndHistory \
      -only-testing:JARVISUITests/JARVISUITests/testRegisterIncomePreviewConfirmAndHistory \
      -only-testing:JARVISUITests/JARVISUITests/testRecurrencePreviewConfirmListAndCancel \
      -only-testing:JARVISUITests/JARVISUITests/testRealAPIRecurrenceSuggestionRequiresExplicitConfirmation \
      -only-testing:JARVISUITests/JARVISUITests/testCreditCardPreviewConfirmDetailAndArchive \
      -only-testing:JARVISUITests/JARVISUITests/testRealAPICardPurchaseAndInstallmentPlanLifecycle
    rm -rf "$real_api_temporary_dir"
    trap - EXIT INT TERM
    safe_available_temporary_dir="$(mktemp -d -t jarvis-safe-available.XXXXXX)"
    safe_available_cleanup() {
      status="$?"
      trap - EXIT INT TERM
      if declare -F safe_available_stop_switch_api >/dev/null 2>&1; then
        safe_available_stop_switch_api || true
      elif [[ -n "${safe_available_switch_api_pid:-}" ]] && kill -0 "$safe_available_switch_api_pid" 2>/dev/null; then
        kill -TERM "$safe_available_switch_api_pid" 2>/dev/null || true
        wait "$safe_available_switch_api_pid" 2>/dev/null || true
      fi
      rm -rf "$safe_available_temporary_dir"
      exit "$status"
    }
    trap safe_available_cleanup EXIT

    safe_available_db_fingerprint() {
      docker compose \
        --project-name "$JARVIS_INTEGRATION_COMPOSE_PROJECT_NAME" \
        --file "$JARVIS_INTEGRATION_COMPOSE_FILE" \
        exec -T postgres \
        psql --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1 \
        --username "$JARVIS_POSTGRES_USER" \
        --dbname "$JARVIS_POSTGRES_DB" <<'SQL' | tr -d '[:space:]'
SELECT (SELECT count(*) FROM transactions) || ':' || COALESCE((SELECT sum(amount_minor) FROM transactions), 0) || '|' ||
       (SELECT count(*) FROM audit_events) || '|' ||
       (SELECT count(*) FROM idempotency_records) || '|' ||
       (SELECT count(*) FROM recurrences) || ':' || COALESCE((SELECT sum(expected_amount_minor) FROM recurrences), 0) || '|' ||
       (SELECT count(*) FROM recurrence_audit_events) || '|' ||
       (SELECT count(*) FROM recurrence_idempotency_records) || '|' ||
       (SELECT count(*) FROM recurrence_suggestion_suppressions) || '|' ||
       (SELECT count(*) FROM credit_cards) || '|' ||
       (SELECT count(*) FROM credit_card_audit_events) || '|' ||
       (SELECT count(*) FROM credit_card_idempotency_records) || '|' ||
       (SELECT count(*) FROM installment_plans) || ':' || COALESCE((SELECT sum(total_minor) FROM installment_plans), 0) || '|' ||
       (SELECT count(*) FROM installment_plan_audit_events) || '|' ||
       (SELECT count(*) FROM installment_plan_idempotency_records) || '|' ||
       (SELECT count(*) FROM card_purchase_idempotency_records) || '|' ||
       (SELECT count(*) FROM monthly_budgets) || ':' || COALESCE((SELECT sum(amount_minor) FROM monthly_budgets), 0);
SQL
    }
    xcodebuild_arguments=(
      test
      -project "$repository_root/apps/ios/JARVIS.xcodeproj"
      -scheme JARVIS
      -destination "platform=iOS Simulator,name=iPhone 15,OS=17.5"
      -derivedDataPath "$safe_available_temporary_dir/DerivedData"
      -parallel-testing-enabled NO
      -maximum-concurrent-test-simulator-destinations 1
      -enableCodeCoverage YES
      CODE_SIGNING_ALLOWED=NO
      JARVIS_IOS_TEST_MODE=real
      "JARVIS_IOS_E2E_BASE_URL=$JARVIS_IOS_E2E_BASE_URL"
      "JARVIS_IOS_E2E_DESCRIPTION=$JARVIS_IOS_E2E_DESCRIPTION"
      "JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION=${JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION:-}"
      "JARVIS_IOS_E2E_SUGGESTION_STARTS_ON=${JARVIS_IOS_E2E_SUGGESTION_STARTS_ON:-}"
      -only-testing:JARVISUITests/JARVISUITests/testRealAPISafeAvailableLifecycle
    )
    monthly_budget_xcodebuild_arguments=(
      test
      -project "$repository_root/apps/ios/JARVIS.xcodeproj"
      -scheme JARVIS
      -destination "platform=iOS Simulator,name=iPhone 15,OS=17.5"
      -derivedDataPath "$safe_available_temporary_dir/DerivedData"
      -parallel-testing-enabled NO
      -maximum-concurrent-test-simulator-destinations 1
      -enableCodeCoverage YES
      CODE_SIGNING_ALLOWED=NO
      JARVIS_IOS_TEST_MODE=real
      "JARVIS_IOS_E2E_BASE_URL=$JARVIS_IOS_E2E_BASE_URL"
      "JARVIS_IOS_E2E_DESCRIPTION=$JARVIS_IOS_E2E_DESCRIPTION"
      "JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION=${JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION:-}"
      "JARVIS_IOS_E2E_SUGGESTION_STARTS_ON=${JARVIS_IOS_E2E_SUGGESTION_STARTS_ON:-}"
      -only-testing:JARVISUITests/JARVISUITests/testRealAPIMonthlyBudgetLifecycle
    )
    echo "Creating SafeAvailable deterministic fixture with the real iOS client (serial, iPhone 15 iOS 17.5)."
    xcodebuild "${xcodebuild_arguments[@]}" \
      -resultBundlePath "$safe_available_temporary_dir/SafeAvailableSetup.xcresult" \
      JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION=__safe_available_setup__

    safe_available_setup_counts_before="$(safe_available_db_fingerprint)"
    printf '%s\n' "$safe_available_setup_counts_before" >"$safe_available_temporary_dir/safe-available-setup-baseline"
    export JARVIS_INTEGRATION_SAFE_AVAILABLE_SETUP_BASELINE_FILE="$safe_available_temporary_dir/safe-available-setup-baseline"

    echo "Running dedicated SafeAvailable real-api XCUITest (read-only, serial, iPhone 15 iOS 17.5)."
    xcodebuild "${xcodebuild_arguments[@]}" \
      -resultBundlePath "$safe_available_temporary_dir/SafeAvailable.xcresult" \
      JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION=__safe_available_read__

    safe_available_setup_counts_after="$(safe_available_db_fingerprint)"
    printf '%s\n' "$safe_available_setup_counts_after" >"$safe_available_temporary_dir/safe-available-setup-after"
    export JARVIS_INTEGRATION_SAFE_AVAILABLE_SETUP_AFTER_FILE="$safe_available_temporary_dir/safe-available-setup-after"

    echo "Creating monthly budget fixture with the real iOS client (serial, iPhone 15 iOS 17.5)."
    xcodebuild "${monthly_budget_xcodebuild_arguments[@]}" \
      -resultBundlePath "$safe_available_temporary_dir/MonthlyBudgetSetup.xcresult" \
      JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION=__monthly_budget_setup__

    safe_available_counts_before="$(safe_available_db_fingerprint)"
    printf '%s\n' "$safe_available_counts_before" >"$safe_available_temporary_dir/safe-available-baseline"
    export JARVIS_INTEGRATION_SAFE_AVAILABLE_BASELINE_FILE="$safe_available_temporary_dir/safe-available-baseline"
    safe_available_budget_month="$(TZ=America/Sao_Paulo node -e 'const now=new Date(); process.stdout.write(`${now.getFullYear()}-${String(now.getMonth()+1).padStart(2,"0")}`);')"
    export JARVIS_IOS_E2E_BUDGET_MONTH="$safe_available_budget_month"

    echo "Running monthly budget cap real-api XCUITest (serial, iPhone 15 iOS 17.5)."
    xcodebuild "${monthly_budget_xcodebuild_arguments[@]}" \
      -resultBundlePath "$safe_available_temporary_dir/MonthlyBudgetRead.xcresult" \
      JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION=__monthly_budget_read__

    if [[ "${JARVIS_IOS_OWNER_ISOLATION_MODE:-false}" == true ]]; then
      owner_b="${JARVIS_INTEGRATION_OWNER_B_ID:?owner B is required for isolation}"
      safe_available_period="$(TZ=America/Sao_Paulo node -e 'const now=new Date(); const y=now.getFullYear(); const m=now.getMonth()+1; const mm=String(m).padStart(2,"0"); const last=new Date(y,m,0).getDate(); process.stdout.write(`periodStart=${y}-${mm}-01&periodEnd=${y}-${mm}-${String(last).padStart(2,"0")}`);')"
      safe_available_base_url="${JARVIS_IOS_E2E_BASE_URL:?real API base URL is required}"
      safe_available_api_port="$(node -e "const u = new URL(process.argv[1]); process.stdout.write(String(u.port || (u.protocol === \"https:\" ? 443 : 80)));" "$safe_available_base_url")"
      safe_available_api_address="127.0.0.1:$safe_available_api_port"
      safe_available_switch_log="$safe_available_temporary_dir/owner-switch-api.log"
      safe_available_switch_api_binary="$safe_available_temporary_dir/jarvis-api-owner-switch"
      safe_available_switch_api_pid=""

      safe_available_stop_switch_api() {
        if [[ -z "$safe_available_switch_api_pid" ]]; then
          return 0
        fi
        if kill -0 "$safe_available_switch_api_pid" 2>/dev/null; then
          kill -TERM "$safe_available_switch_api_pid" 2>/dev/null || true
          for _ in {1..50}; do
            if ! kill -0 "$safe_available_switch_api_pid" 2>/dev/null; then
              break
            fi
            sleep 0.1
          done
          if kill -0 "$safe_available_switch_api_pid" 2>/dev/null; then
            echo "Owner-switch API did not stop gracefully." >&2
            kill -KILL "$safe_available_switch_api_pid" 2>/dev/null || true
            return 1
          fi
        fi
        wait "$safe_available_switch_api_pid" 2>/dev/null || true
        safe_available_switch_api_pid=""
      }

      safe_available_stop_outer_api() {
        local api_pid api_command
        api_pid_lines="$(lsof -ti tcp:"$safe_available_api_port")"
        if [[ -z "$api_pid_lines" ]]; then
          echo "Could not identify the owner A API process on $safe_available_api_address." >&2
          return 1
        fi
        if [[ "$(printf '%s\n' "$api_pid_lines" | wc -l | tr -d '[:space:]')" != 1 ]]; then
          echo "Multiple listeners found on $safe_available_api_address." >&2
          return 1
        fi
        api_pid="$(printf '%s' "$api_pid_lines" | tr -d '[:space:]')"
        api_command="$(ps -p "$api_pid" -o command=)"
        if [[ "$api_command" != *jarvis-api* ]]; then
          echo "Refusing to stop non-J.A.R.V.I.S. process on $safe_available_api_address." >&2
          return 1
        fi
        kill -TERM "$api_pid"
        for _ in {1..50}; do
          if ! kill -0 "$api_pid" 2>/dev/null; then
            return 0
          fi
          sleep 0.1
        done
        echo "Owner A API did not stop gracefully." >&2
        kill -KILL "$api_pid" 2>/dev/null || true
        return 1
      }

      safe_available_start_switch_api() {
        local owner="$1"
        JARVIS_HTTP_ADDRESS="$safe_available_api_address" \
        JARVIS_SHUTDOWN_TIMEOUT="5s" \
        JARVIS_FINANCIAL_API_ENABLED="true" \
        JARVIS_OWNER_ID="$owner" \
          "$safe_available_switch_api_binary" >>"$safe_available_switch_log" 2>&1 &
        safe_available_switch_api_pid=$!
        for _ in {1..50}; do
          if ! kill -0 "$safe_available_switch_api_pid" 2>/dev/null; then
            echo "Owner-switch API exited before readiness." >&2
            return 1
          fi
          if curl --fail --silent --show-error --connect-timeout 1 --max-time 2 \
            "$safe_available_base_url/healthz" >/dev/null 2>&1; then
            return 0
          fi
          sleep 0.1
        done
        echo "Owner-switch API readiness timed out." >&2
        return 1
      }

      safe_available_capture_response() {
        local label="$1"
        local output_file="$2"
        local status
        status="$(curl --silent --show-error --output "$output_file" --write-out '%{http_code}' \
          --connect-timeout 2 --max-time 5 -H 'Accept: application/json' \
          "$safe_available_base_url/v1/safe-available?$safe_available_period")"
        if [[ "$status" != 200 ]]; then
          echo "SafeAvailable $label request returned HTTP $status." >&2
          return 1
        fi
      }

      safe_available_assert_owner_b_response() {
        node - "$1" <<'NODE'
const fs = require('fs');
const body = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
if (!body || !body.finalAmount || body.finalAmount.minor !== 0 || body.finalAmount.currency !== 'BRL') process.exit(1);
for (const key of ['availableBalance', 'totalConfirmedIncome', 'totalConfirmedExpense', 'totalConfirmedCommitments']) {
  if (!body[key] || body[key].minor !== 0 || body[key].currency !== 'BRL') process.exit(1);
}
if (!Array.isArray(body.breakdown) || body.breakdown.length !== 1 || body.breakdown[0].kind !== 'AVAILABLE_BALANCE' || body.breakdown[0].amount.minor !== 0) process.exit(1);
if (!Array.isArray(body.missingData) || !body.missingData.includes('BUDGET')) process.exit(1);
if (JSON.stringify(body).includes('safe_') || JSON.stringify(body).includes('card_') || JSON.stringify(body).includes('rec_')) process.exit(1);
NODE
      }

      safe_available_assert_owner_a_response() {
        node - "$1" "$2" <<'NODE'
const fs = require('fs');
const before = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const after = JSON.parse(fs.readFileSync(process.argv[3], 'utf8'));
if (JSON.stringify(before) !== JSON.stringify(after)) process.exit(1);
if (!after || !after.finalAmount || !Number.isInteger(after.finalAmount.minor) || after.finalAmount.currency !== 'BRL') process.exit(1);
if (!Array.isArray(after.breakdown) || after.breakdown.length < 2) process.exit(1);
if (!Array.isArray(after.missingData) || !after.missingData.includes('BUDGET')) process.exit(1);
for (const key of ['availableBalance', 'totalConfirmedIncome', 'totalConfirmedExpense', 'totalConfirmedCommitments']) {
  if (!after[key] || !Number.isInteger(after[key].minor) || after[key].currency !== 'BRL') process.exit(1);
}
console.log(`SafeAvailable owner A totals: available=${after.availableBalance.minor}, income=${after.totalConfirmedIncome.minor}, expense=${after.totalConfirmedExpense.minor}, commitments=${after.totalConfirmedCommitments.minor}, final=${after.finalAmount.minor}`);
NODE
      }

      safe_available_assert_owner_a_budget_response() {
        node - "$1" "$2" <<'NODE'
const fs = require('fs');
const before = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const after = JSON.parse(fs.readFileSync(process.argv[3], 'utf8'));
if (JSON.stringify(before) !== JSON.stringify(after)) process.exit(1);
if (!after || !after.budget || after.budget.minor !== 7000 || after.budget.currency !== 'BRL') process.exit(1);
if (!after.budgetRemaining || after.budgetRemaining.currency !== 'BRL' || !Number.isInteger(after.budgetRemaining.minor)) process.exit(1);
if (!after.finalAmount || after.finalAmount.currency !== 'BRL' || !Number.isInteger(after.finalAmount.minor)) process.exit(1);
if (!Array.isArray(after.breakdown) || after.breakdown.length !== 11) process.exit(1);
if (!Array.isArray(after.missingData) || after.missingData.includes('BUDGET')) process.exit(1);
if (!after.breakdown.some((line) => line.kind === 'INCOME' && line.amount && line.amount.minor === 100000)) process.exit(1);
console.log(`SafeAvailable owner A with budget: final=${after.finalAmount.minor}, budgetRemaining=${after.budgetRemaining.minor}, breakdown=${after.breakdown.length}`);
NODE
      }

      if ! docker compose \
        --project-name "$JARVIS_INTEGRATION_COMPOSE_PROJECT_NAME" \
        --file "$JARVIS_INTEGRATION_COMPOSE_FILE" \
        exec -T postgres \
        psql --quiet --set=ON_ERROR_STOP=1 --username "$JARVIS_POSTGRES_USER" --dbname "$JARVIS_POSTGRES_DB" \
        --set=owner_id="$owner_b" <<'SQL'; then
INSERT INTO users (id, created_at, updated_at)
VALUES (:'owner_id', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO NOTHING;
SQL
        echo "Could not seed owner B for SafeAvailable isolation." >&2
        return 1
      fi

      (
        cd "$repository_root/backend"
        "${GO:-go}" build -trimpath -o "$safe_available_switch_api_binary" ./cmd/api
      )

      safe_available_a_before="$safe_available_temporary_dir/safe-a-before.json"
      safe_available_b="$safe_available_temporary_dir/safe-b.json"
      safe_available_a_after="$safe_available_temporary_dir/safe-a-after.json"
      safe_available_capture_response "owner A before switch" "$safe_available_a_before"
      node - "$safe_available_a_before" <<'NODE'
const fs = require('fs');
const body = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
      if (!body || !body.budget || body.budget.minor !== 7000 || body.budget.currency !== 'BRL') process.exit(1);
      if (!body.budgetRemaining || body.budgetRemaining.currency !== 'BRL' || !Number.isInteger(body.budgetRemaining.minor)) process.exit(1);
      if (!body.finalAmount || !Number.isInteger(body.finalAmount.minor) || body.finalAmount.currency !== 'BRL') process.exit(1);
      if (!Array.isArray(body.breakdown) || body.breakdown.length !== 11) process.exit(1);
      if (!Array.isArray(body.missingData) || body.missingData.includes('BUDGET')) process.exit(1);
      if (!body.breakdown.some((line) => line.kind === 'INCOME' && line.amount && line.amount.minor === 100000)) process.exit(1);
      console.log(`SafeAvailable owner A with budget: finalAmount.minor=${body.finalAmount.minor}, budgetRemaining=${body.budgetRemaining.minor}, breakdown=${body.breakdown.length}`);
NODE

      safe_available_stop_outer_api
      safe_available_start_switch_api "$owner_b"
      safe_available_capture_response "owner B" "$safe_available_b"
      safe_available_assert_owner_b_response "$safe_available_b"
      budget_b_status="$(curl --silent --show-error --output "$safe_available_temporary_dir/budget-b.json" --write-out '%{http_code}' \
        --connect-timeout 2 --max-time 5 -H 'Accept: application/json' \
        "$safe_available_base_url/v1/monthly-budgets/$safe_available_budget_month")"
      if [[ "$budget_b_status" != 404 ]] || ! grep -q 'MONTHLY_BUDGET_NOT_FOUND' "$safe_available_temporary_dir/budget-b.json"; then
        echo "Owner B monthly budget read did not return sanitized 404." >&2
        return 1
      fi
      safe_available_counts_after_b="$(safe_available_db_fingerprint)"
      if [[ "$safe_available_counts_after_b" != "$safe_available_counts_before" ]]; then
        echo "SafeAvailable owner B read changed database state: before=$safe_available_counts_before after=$safe_available_counts_after_b" >&2
        return 1
      fi
      echo "SafeAvailable owner B: HTTP 200, zero result, BUDGET marker, no owner A data, zero writes."

      safe_available_stop_switch_api
      safe_available_start_switch_api "$JARVIS_INTEGRATION_OWNER_ID"
      safe_available_capture_response "owner A after switch" "$safe_available_a_after"
      safe_available_assert_owner_a_budget_response "$safe_available_a_before" "$safe_available_a_after"
      budget_a_status="$(curl --silent --show-error --output "$safe_available_temporary_dir/budget-a.json" --write-out '%{http_code}' \
        --connect-timeout 2 --max-time 5 -H 'Accept: application/json' \
        "$safe_available_base_url/v1/monthly-budgets/$safe_available_budget_month")"
      if [[ "$budget_a_status" != 200 ]] || ! grep -q '"minor":7000' "$safe_available_temporary_dir/budget-a.json"; then
        echo "Owner A monthly budget read did not preserve the configured value." >&2
        return 1
      fi
      safe_available_capture_response "owner A repeated read" "$safe_available_temporary_dir/safe-a-repeated.json"
      safe_available_assert_owner_a_budget_response "$safe_available_a_before" "$safe_available_temporary_dir/safe-a-repeated.json"
      safe_available_counts_after_a="$(safe_available_db_fingerprint)"
      if [[ "$safe_available_counts_after_a" != "$safe_available_counts_before" ]]; then
        echo "SafeAvailable owner A reads changed database state: before=$safe_available_counts_before after=$safe_available_counts_after_a" >&2
        return 1
      fi
      echo "SafeAvailable owner A after restart: response and breakdown identical, repeated read produced no writes."
      safe_available_stop_switch_api
    fi

    bash "$repository_root/scripts/check-ios-integration-postcondition.sh"
JARVIS_INNER
bash "$repository_root/scripts/test-integration.sh" \
  bash "$inner_script" "$repository_root"
