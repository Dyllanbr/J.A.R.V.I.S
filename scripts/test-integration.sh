#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="$repository_root/compose.yaml"
project_name="jarvis-it-$$-$RANDOM"
go_bin="${GO:-go}"
cleanup_required=false

export JARVIS_POSTGRES_DB="jarvis_test"
export JARVIS_POSTGRES_USER="jarvis_test"
export JARVIS_POSTGRES_PASSWORD="jarvis_synthetic_$$_$RANDOM"
export JARVIS_POSTGRES_PORT="0"

cleanup() {
  status=$?
  trap - EXIT INT TERM

  if [[ "$cleanup_required" == true ]]; then
    if ((status != 0)); then
      docker compose --project-name "$project_name" --file "$compose_file" logs --no-color postgres >&2 || true
    fi

    if ! docker compose --project-name "$project_name" --file "$compose_file" down --timeout 10 --volumes --remove-orphans >/dev/null; then
      echo "PostgreSQL integration cleanup failed." >&2
      if ((status == 0)); then
        status=1
      fi
    fi
  fi

  exit "$status"
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  echo "PostgreSQL integration tests require a running Docker daemon." >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "PostgreSQL integration tests require Docker Compose." >&2
  exit 1
fi

cleanup_required=true
docker compose \
  --project-name "$project_name" \
  --file "$compose_file" \
  up --detach --wait --wait-timeout 45 postgres

published_address="$(docker compose \
  --project-name "$project_name" \
  --file "$compose_file" \
  port postgres 5432)"
published_port="${published_address##*:}"
if [[ ! "$published_port" =~ ^[0-9]+$ ]] || ((published_port < 1 || published_port > 65535)); then
  echo "Docker did not publish a valid PostgreSQL test port." >&2
  exit 1
fi

export JARVIS_TEST_DATABASE_URL="postgres://${JARVIS_POSTGRES_USER}:${JARVIS_POSTGRES_PASSWORD}@127.0.0.1:${published_port}/${JARVIS_POSTGRES_DB}?sslmode=disable"

cd "$repository_root/backend"
"$go_bin" test -race -cover -count=1 -tags=integration ./internal/modules/transactions/adapters/postgres
