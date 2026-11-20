#!/bin/sh
set -eu

connector="$CDC_CONNECTOR"
url="$CONNECT_URL/connectors/$connector"

curl -fsS -o /dev/null -X PUT -H 'Content-Type: application/json' --data @/cdc/outbox-connector.json "$url/config"
echo "cdc: connector $connector configured"

attempt=0
while :; do
	status=$(curl -fs "$url/status" || true)
	case "$status" in
	*'"tasks":[{"id":0,"state":"RUNNING"'*)
		echo "cdc: connector $connector is running"
		exit 0
		;;
	*'"state":"FAILED"'*)
		echo "cdc: connector $connector failed, restarting: $status" >&2
		curl -fsS -o /dev/null -X POST "$url/restart?includeTasks=true&onlyFailed=true" || true
		;;
	esac
	attempt=$((attempt + 1))
	if [ "$attempt" -ge "${CDC_WAIT_ATTEMPTS:-60}" ]; then
		echo "cdc: connector $connector did not reach RUNNING: ${status:-no status}" >&2
		exit 1
	fi
	sleep 2
done
