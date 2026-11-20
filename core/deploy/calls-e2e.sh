#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
export CALLS_EVIDENCE_SUBDIR=CI
mkdir -p .local/evidence/calls/CI
docker compose -f core/deploy/docker-compose.tools.yml run --rm buf generate
docker compose -f core/deploy/docker-compose.calls-spike.yml build init
docker run --rm --network none --user "$(id -u):$(id -g)" --entrypoint node -e TURN_LISTEN_IP=172.30.252.20 -e TURN_ALLOWED_PEERS=172.30.252.0-172.30.252.255 -v "$PWD/ui/web/e2e:/app/e2e:ro" -v "$PWD/.local/evidence/calls/CI:/evidence" axon-calls-spike-playwright:local /app/e2e/calls-spike-init.mjs
stack=(docker compose -p axon-calls-e2e --env-file .local/evidence/calls/CI/.secrets/calls.env -f core/deploy/docker-compose.web-e2e.yml -f core/deploy/docker-compose.calls-e2e.yml)
trap '"${stack[@]}" down -v' EXIT
"${stack[@]}" up -d --build auth chat gateway web coturn
for config in playwright.calls-ws.config.ts playwright.calls-ui.config.ts; do
  CALLS_PLAYWRIGHT_CONFIG="$config" "${stack[@]}" run --rm playwright
done
