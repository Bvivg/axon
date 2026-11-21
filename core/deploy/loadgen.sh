#!/usr/bin/env bash
set -euo pipefail

usage() {
	echo "usage: $0 bench|chaos docker compose -p axon-<purpose> -f core/deploy/docker-compose.yml [-f overlay ...] -f core/deploy/docker-compose.loadgen.yml --profile load" >&2
	exit 64
}

mode=${1:-}
case "$mode" in
bench | chaos) ;;
*) usage ;;
esac
shift
[ "$#" -gt 0 ] || usage
stack=("$@")
here=$(cd "$(dirname "$0")" && pwd)
command -v jq >/dev/null || {
	echo "$mode: jq is required" >&2
	exit 69
}

if [ "$mode" = chaos ]; then
	matches=${MATCHES:-200}
	concurrency=${CONCURRENCY:-10}
	seed=${SEED:-21}
else
	matches=${MATCHES:-6000}
	concurrency=${CONCURRENCY:-1000}
	seed=${SEED:-11}
fi
pace=${PACE:-260ms}
players=$((concurrency * 2))
reports=${REPORT_DIR:-.local/$mode}
log_file=${LOG_FILE:-e2e-logs.txt}
mkdir -p "$reports"
rm -f "$reports/loadgen.json" "$reports/reconcile.json"

step() { echo "$mode: $(date +%T) $*"; }

load=""
finish() {
	local status=$?
	if [ -n "$load" ]; then kill "$load" 2>/dev/null || true; fi
	if [ "$status" -ne 0 ]; then
		"${stack[@]}" logs --no-color >"$log_file" 2>&1 || true
		step "failed with status $status, stack logs in $log_file"
	fi
	if [ "${KEEP:-}" = 1 ]; then
		step "KEEP=1, the stack stays up; remove it with: ${stack[*]} down -v --remove-orphans"
	else
		"${stack[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
	fi
	exit "$status"
}
trap finish EXIT

faults() {
	local replicas toxiproxy
	read -r -a replicas <<<"$("${stack[@]}" ps --format '{{.Name}}' game | sort | tr '\n' ' ')"
	if [ "${#replicas[@]}" -lt 2 ]; then
		step "chaos needs two game replicas, found ${#replicas[@]}"
		return 1
	fi
	toxiproxy=$("${stack[@]}" port toxiproxy 8474)
	export TOXIPROXY_URL="http://$toxiproxy"
	sleep "${WARMUP:-5}"
	step "postgres latency ${LATENCY_MS:-300} ms for ${HOLD:-15} s"
	HOLD=${HOLD:-15} "$here/chaos/scenario.sh" postgres-latency
	sleep 5
	step "graceful restart of ${replicas[0]}"
	DOWN=5 "$here/chaos/scenario.sh" restart "${replicas[0]}"
	sleep 10
	step "kill of ${replicas[1]}"
	DOWN=5 "$here/chaos/scenario.sh" kill "${replicas[1]}"
}

step "building and starting the stack"
"${stack[@]}" up -d --build --wait --wait-timeout 300 game

step "loadgen: $matches matches, $concurrency in flight, pace $pace, seed $seed"
"${stack[@]}" run --rm -T loadgen -target http://game:8083 -players "$players" -matches "$matches" \
	-concurrency "$concurrency" -pace "$pace" -seed "$seed" -out /results/results.json >"$reports/loadgen.json" &
load=$!

if [ "$mode" = chaos ]; then
	faults
	if ! kill -0 "$load" 2>/dev/null; then
		step "loadgen finished before the last fault; raise MATCHES or lower CONCURRENCY"
		wait "$load" || true
		load=""
		exit 1
	fi
	step "faults done, waiting for loadgen"
fi
wait "$load"
load=""

jq -r --arg mode "$mode" '"\($mode): \(.matches) matches, \(.failed) failed, \(.moves) moves in \(.elapsed_s) s; \(.moves_per_sec) moves/s; latency p50/p95/p99 \(.latency_ms.p50)/\(.latency_ms.p95)/\(.latency_ms.p99) ms; errors \(.errors | tojson)"' "$reports/loadgen.json"

status=0
"${stack[@]}" run --rm -T reconcile -results /results/results.json -min-completed "${MIN_COMPLETED:-0.95}" >"$reports/reconcile.json" || status=$?
if [ -s "$reports/reconcile.json" ]; then
	jq -r --arg mode "$mode" '"\($mode): confirmed \(.confirmed_moves) moves, journal \(.journal_moves), read model \(.read_model_moves); lost \(.sessions_with_lost_moves), diverged \(.sessions_diverged), unsettled \(.completed_unsettled); completed \(.completed_and_settled)/\(.matches)"' "$reports/reconcile.json"
fi
exit "$status"
