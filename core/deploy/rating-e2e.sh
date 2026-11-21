#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
	echo "usage: $0 docker compose -p axon-<purpose> -f core/deploy/docker-compose.yml -f core/deploy/docker-compose.rating-e2e.yml [--env-file ...]" >&2
	exit 64
fi
stack=("$@")
games=${RATING_E2E_OUTAGE_GAMES:-50}
delay=${RATING_E2E_OUTAGE_DELAY:-12}
outage=${RATING_E2E_OUTAGE_SECONDS:-60}

"${stack[@]}" up -d --build --wait postgres redis kafka minio edge-redis connect registry auth chat game rating gateway
"${stack[@]}" run --rm cdc-connector
"${stack[@]}" run --rm schema-register

RATING_E2E_RUN='TestRatedQuickMatchTravelsTheOutboxChain|TestOutboxRowsCannotPublishAnotherServicesTopic' "${stack[@]}" run --rm rating-e2e

RATING_E2E_RUN=TestRatedGamesSurviveAnOutboxOutage RATING_E2E_OUTAGE_GAMES="$games" "${stack[@]}" run --rm rating-e2e &
runner=$!
sleep "$delay"
echo "rating-e2e: $(date +%T) stopping connect for ${outage}s"
"${stack[@]}" stop connect
sleep "$outage"
echo "rating-e2e: $(date +%T) starting connect"
"${stack[@]}" start connect
for _ in $(seq 60); do
	if "${stack[@]}" exec -T connect curl -fs http://127.0.0.1:8083/connectors/axon-outbox/status 2>/dev/null | grep -q '"tasks":\[{"id":0,"state":"RUNNING"'; then
		echo "rating-e2e: $(date +%T) connector axon-outbox is running again"
		break
	fi
	sleep 2
done
wait "$runner"

"${stack[@]}" run --rm --no-deps rating -rebuild
echo "rating-e2e: rebuild from game.finished.v1 matches rating.ratings and the leaderboards"
