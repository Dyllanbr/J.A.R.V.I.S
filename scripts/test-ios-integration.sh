#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export JARVIS_IOS_E2E_DESCRIPTION="Mercado_sintetico_E2E_$$_$RANDOM"

bash "$repository_root/scripts/test-integration.sh" \
  bash -c '
    set -euo pipefail
    repository_root="$1"
    bash "$repository_root/scripts/test-ios.sh" --real-api
    bash "$repository_root/scripts/check-ios-integration-postcondition.sh"
  ' _ "$repository_root"
