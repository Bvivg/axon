#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
export CALLS_EVIDENCE_SUBDIR="${CALLS_EVIDENCE_SUBDIR:-R3-ci}"
if [[ ! "$CALLS_EVIDENCE_SUBDIR" =~ ^[a-zA-Z0-9_-]+$ ]]; then exit 2; fi
if [[ -n "$(docker ps -aq --filter label=com.docker.compose.project=axon-calls-e2e)" ]]; then
  echo "Existing axon-calls-e2e project found; refusing to replace its fixtures."
  exit 2
fi
evidence="$PWD/.local/evidence/calls/$CALLS_EVIDENCE_SUBDIR"
mkdir -p "$evidence"
docker compose -f core/deploy/docker-compose.tools.yml run --rm buf generate
docker compose -f core/deploy/docker-compose.calls-spike.yml build init
docker run --rm --network none --user "$(id -u):$(id -g)" --entrypoint node -e TURN_LISTEN_IP=172.30.252.20 -e TURN_ALLOWED_PEERS=172.30.252.0-172.30.252.255 -v "$PWD/ui/web/e2e:/app/e2e:ro" -v "$evidence:/evidence" axon-calls-spike-playwright:local /app/e2e/calls-spike-init.mjs
docker run --rm --network none --user "$(id -u):$(id -g)" --entrypoint node -v "$PWD/ui/web/e2e:/app/e2e:ro" -v "$evidence:/evidence" node:22-alpine /app/e2e/calls-admission-init.mjs
stack=(docker compose -p axon-calls-e2e --env-file "$evidence/.secrets/calls.env" -f core/deploy/docker-compose.web-e2e.yml -f core/deploy/docker-compose.calls-e2e.yml -f core/deploy/docker-compose.calls-admission.yml -f core/deploy/docker-compose.calls-sfu.yml -f core/deploy/docker-compose.calls-migration.yml)
trap '"${stack[@]}" down -v' EXIT
"${stack[@]}" up -d --build auth chat gateway web coturn livekit
"${stack[@]}" run --rm playwright
if [[ "${CALLS_VERIFY_REFRESH:-0}" == "1" ]]; then
  "${stack[@]}" run --rm --no-deps -e CALLS_REFRESH_MIGRATION=1 -v "$PWD/ui/web/scripts/calls-sfu-refresh.mjs:/app/calls-sfu-refresh.mjs:ro" --entrypoint node playwright /app/calls-sfu-refresh.mjs
fi
