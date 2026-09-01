#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
project="$repository_root/apps/ios/JARVIS.xcodeproj"
scheme="JARVIS"
bundle_id="dev.jarvis.JARVIS"
mode="${1:---all}"
temporary_dir="$(mktemp -d -t jarvis-ios-tests.XXXXXX)"
derived_data="$temporary_dir/DerivedData"
result_bundle="$temporary_dir/JARVIS.xcresult"
booted_by_script=false
simulator_id=""
simulator_runtime=""

simulator_state() {
  xcrun simctl list devices | awk -v simulator_id="$simulator_id" '
    index($0, simulator_id) > 0 {
      if ($0 ~ /\(Booted\)/) print "Booted"
      if ($0 ~ /\(Shutdown\)/) print "Shutdown"
      exit
    }
  '
}

wait_for_simulator_shutdown() {
  for _ in {1..60}; do
    if [[ "$(simulator_state)" == "Shutdown" ]]; then
      sleep 0.5
      [[ "$(simulator_state)" == "Shutdown" ]] && return 0
    fi
    sleep 0.1
  done
  return 1
}

cleanup() {
  status="$1"
  trap - EXIT INT TERM

  if ((status != 0)) && [[ -d "$result_bundle" ]]; then
    echo "iOS test failures:" >&2
    xcrun xcresulttool get test-results tests \
      --path "$result_bundle" \
      --format json >&2 || true
  fi

  if [[ -n "$simulator_id" ]]; then
    xcrun simctl terminate "$simulator_id" "$bundle_id" >/dev/null 2>&1 || true
    xcrun simctl uninstall "$simulator_id" "$bundle_id" >/dev/null 2>&1 || true
    if [[ "$booted_by_script" == true ]]; then
      xcrun simctl shutdown "$simulator_id" >/dev/null 2>&1 || true
      if ! wait_for_simulator_shutdown; then
        xcrun simctl shutdown "$simulator_id" >/dev/null 2>&1 || true
        if ! wait_for_simulator_shutdown; then
          echo "Simulator $simulator_id did not reach a stable Shutdown state." >&2
          if ((status == 0)); then
            status=1
          fi
        fi
      fi
    fi
  fi

  rm -rf "$temporary_dir"
  exit "$status"
}

trap 'cleanup "$?"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

case "$mode" in
  --all | --unit | --ui | --recurrence-ui | --card-ui | --tab-regression | --card-preview-real-api | --real-api) ;;
  *)
    echo "Usage: scripts/test-ios.sh [--all|--unit|--ui|--recurrence-ui|--card-ui|--tab-regression|--card-preview-real-api|--real-api]" >&2
    exit 2
    ;;
esac

for command_name in xcodebuild xcrun; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "iOS tests require Xcode command-line tools ($command_name was not found)." >&2
    exit 1
  fi
done

toolchain_version="$(xcodebuild -version)"
xcode_major="$(printf '%s\n' "$toolchain_version" | awk 'NR == 1 { split($2, version, "."); print version[1] }')"
if [[ ! "$xcode_major" =~ ^[0-9]+$ ]] || ((xcode_major < 16)); then
  echo "J.A.R.V.I.S. requires Xcode 16 or newer; found: $toolchain_version" >&2
  exit 1
fi
printf '%s\n' "$toolchain_version"

runtime_pattern='^-- iOS ([0-9]+)\.([0-9]+)(\.[0-9]+)? --$'
device_pattern='^[[:space:]]+(iPhone[^()]*)[[:space:]]+\(([0-9A-Fa-f-]+)\)[[:space:]]+\((Booted|Shutdown)\)[[:space:]]*$'
candidates=""
runtime_major=0
runtime_minor=0
runtime_name=""
while IFS= read -r line; do
  if [[ "$line" =~ $runtime_pattern ]]; then
    runtime_major="${BASH_REMATCH[1]}"
    runtime_minor="${BASH_REMATCH[2]}"
    runtime_name="iOS ${BASH_REMATCH[1]}.${BASH_REMATCH[2]}${BASH_REMATCH[3]}"
    continue
  fi
  if ((runtime_major < 17)) || [[ ! "$line" =~ $device_pattern ]]; then
    continue
  fi

  simulator_name="$(printf '%s' "${BASH_REMATCH[1]}" | sed -E 's/[[:space:]]+$//')"
  candidate_id="${BASH_REMATCH[2]}"
  candidate_state="${BASH_REMATCH[3]}"
  preference=1
  if [[ "$simulator_name" == "iPhone 15" ]]; then
    preference=0
  fi
  candidates+="${preference}"$'\t'"${runtime_major}"$'\t'"${runtime_minor}"$'\t'"${simulator_name}"$'\t'"${candidate_id}"$'\t'"${candidate_state}"$'\t'"${runtime_name}"$'\n'
done < <(xcrun simctl list devices available)

selected="$(printf '%s' "$candidates" | LC_ALL=C sort -t $'\t' -k1,1n -k2,2nr -k3,3nr -k4,4 -k5,5 | head -n 1)"
if [[ -z "$selected" ]]; then
  echo "No available iPhone Simulator was found. Install a compatible iOS runtime in Xcode." >&2
  exit 1
fi
IFS=$'\t' read -r _ _ _ simulator_name simulator_id simulator_original_state simulator_runtime <<<"$selected"

if [[ "$simulator_original_state" == "Shutdown" ]]; then
  xcrun simctl boot "$simulator_id"
  booted_by_script=true
fi
xcrun simctl bootstatus "$simulator_id" -b

echo "Using iOS Simulator: device=$simulator_name runtime=$simulator_runtime UDID=$simulator_id"

test_filters=()
test_api_mode="stub"
test_description="Mercado_sintetico_UI"
case "$mode" in
  --unit)
    test_filters+=("-only-testing:JARVISTests")
    ;;
  --ui)
    test_filters+=("-only-testing:JARVISUITests")
    ;;
  --recurrence-ui)
    test_filters+=(
      "-only-testing:JARVISUITests/JARVISUITests/testRecurrencePreviewConfirmListAndCancel"
      "-only-testing:JARVISUITests/JARVISUITests/testRecurrenceSuggestionAppearsRequiresReviewAndConfirmsThroughCanonicalFlow"
      "-only-testing:JARVISUITests/JARVISUITests/testRecurrenceSuggestionDismissRequiresConfirmationAndDoesNotAffectConfirmedItems"
      "-only-testing:JARVISUITests/JARVISUITests/testStaleRecurrenceSuggestionIsRemovedWithoutInvalidReviewNavigation"
    )
    ;;
  --card-ui)
    test_filters+=(
      "-only-testing:JARVISUITests/JARVISUITests/testCreditCardPreviewConfirmDetailAndArchive"
      "-only-testing:JARVISUITests/JARVISUITests/testCreditCardFailureExposesSafeRetry"
    )
    ;;
  --tab-regression)
    test_filters+=(
      "-only-testing:JARVISUITests/JARVISUITests/testTabIdentifiersSurviveSuccessAndRepeatedNavigation"
    )
    ;;
  --card-preview-real-api | --real-api)
    if [[ -z "${JARVIS_IOS_E2E_BASE_URL:-}" ]]; then
      echo "--real-api requires JARVIS_IOS_E2E_BASE_URL." >&2
      exit 1
    fi
    case "$JARVIS_IOS_E2E_BASE_URL" in
      http://* | https://*) ;;
      *)
        echo "--real-api requires an HTTP or HTTPS base URL." >&2
        exit 1
        ;;
    esac
    test_api_mode="real"
    test_description="${JARVIS_IOS_E2E_DESCRIPTION:-Mercado_sintetico_E2E_$$_$RANDOM}"
    if [[ "$mode" == "--card-preview-real-api" ]]; then
      test_filters+=(
        "-only-testing:JARVISUITests/JARVISUITests/testRealAPICreditCardPreviewStopsBeforeConfirmation"
      )
    else
      test_filters+=(
        "-only-testing:JARVISUITests/JARVISUITests/testRegisterPreviewConfirmAndHistory"
        "-only-testing:JARVISUITests/JARVISUITests/testRegisterIncomePreviewConfirmAndHistory"
        "-only-testing:JARVISUITests/JARVISUITests/testRecurrencePreviewConfirmListAndCancel"
        "-only-testing:JARVISUITests/JARVISUITests/testRealAPIRecurrenceSuggestionRequiresExplicitConfirmation"
        "-only-testing:JARVISUITests/JARVISUITests/testCreditCardPreviewConfirmDetailAndArchive"
      )
    fi
    ;;
esac

echo "Running iOS UI configuration: API mode=$test_api_mode"

xcodebuild_arguments=(
  test
  -quiet
  -project "$project"
  -scheme "$scheme"
  -destination "platform=iOS Simulator,id=$simulator_id"
  -derivedDataPath "$derived_data"
  -resultBundlePath "$result_bundle"
  -enableCodeCoverage YES
  CODE_SIGNING_ALLOWED=NO
  "JARVIS_IOS_TEST_MODE=$test_api_mode"
  "JARVIS_IOS_E2E_BASE_URL=${JARVIS_IOS_E2E_BASE_URL:-}"
  "JARVIS_IOS_E2E_DESCRIPTION=$test_description"
  "JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION=${JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION:-}"
  "JARVIS_IOS_E2E_SUGGESTION_STARTS_ON=${JARVIS_IOS_E2E_SUGGESTION_STARTS_ON:-}"
)
if ((${#test_filters[@]} > 0)); then
  for test_filter in "${test_filters[@]}"; do
    xcodebuild_arguments+=("$test_filter")
  done
fi
xcodebuild "${xcodebuild_arguments[@]}"

test_results="$(xcrun xcresulttool get test-results tests --path "$result_bundle" --format json)"
if grep -Fq "Invalid frame dimension (negative or non-finite)." <<<"$test_results"; then
  echo "The iOS test run reported an invalid app layout frame." >&2
  exit 1
fi

if [[ "$mode" == "--all" || "$mode" == "--unit" ]]; then
  echo "iOS line coverage summary:"
  xcrun xccov view --report --only-targets "$result_bundle"
fi

echo "iOS tests passed."
