#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export JARVIS_IOS_E2E_DESCRIPTION="Mercado_sintetico_E2E_$$_$RANDOM"
export JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION="${JARVIS_IOS_E2E_DESCRIPTION}_suggestion"

bash "$repository_root/scripts/test-integration.sh" \
  bash -c '
    set -euo pipefail
    repository_root="$1"
    export JARVIS_IOS_E2E_SUGGESTION_STARTS_ON="$(node "$repository_root/scripts/prepare-ios-recurrence-suggestion-e2e.mjs")"
    bash "$repository_root/scripts/test-ios.sh" --card-preview-real-api
    bash "$repository_root/scripts/check-ios-credit-card-preview-postcondition.sh"
    bash "$repository_root/scripts/test-ios.sh" --real-api
    bash "$repository_root/scripts/check-ios-integration-postcondition.sh"
  ' _ "$repository_root"
