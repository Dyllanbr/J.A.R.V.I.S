#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
owner_isolation_mode=false
if (($# > 1)); then
  echo "Usage: scripts/test-ios-integration.sh [--owner-isolation]" >&2
  exit 2
fi
if (($# == 1)); then
  case "$1" in
    --owner-isolation)
      owner_isolation_mode=true
      ;;
    *)
      echo "Usage: scripts/test-ios-integration.sh [--owner-isolation]" >&2
      exit 2
      ;;
  esac
fi
export JARVIS_IOS_E2E_DESCRIPTION="Mercado_sintetico_E2E_$$_$RANDOM"
export JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION="${JARVIS_IOS_E2E_DESCRIPTION}_suggestion"
if [[ "$owner_isolation_mode" == true ]]; then
  export JARVIS_INTEGRATION_OWNER_ISOLATION=true
fi

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
