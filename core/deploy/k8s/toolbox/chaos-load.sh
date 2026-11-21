#!/usr/bin/env bash
set -euo pipefail

work=/tmp/load
matches=${MATCHES:-2000}
concurrency=${CONCURRENCY:-150}
pace=${PACE:-260ms}
seed=${SEED:-21}
partitions=${GAME_JOURNAL_PARTITIONS:-24}
topic=${GAME_JOURNAL_TOPIC:-game.session-events.v1}
target=${LOAD_TARGET:-http://127.0.0.1:18083}
mkdir -p "$work"

for _ in $(seq 1 60); do
  curl -s -o /dev/null "$target/" && break
  sleep 1
done

echo "$(date +%T) load started: $matches matches, $concurrency in flight"
loadgen -target "$target" -players $((concurrency * 2)) \
  -out "$work/results.json" -matches "$matches" -concurrency "$concurrency" -pace "$pace" -seed "$seed" \
  >"$work/run.log" 2>&1 || true
echo "$(date +%T) load finished"
sed -n '/^{/,/^}/p' "$work/run.log" | jq -c '{matches, failed, elapsed_s, moves, moves_per_sec, latency_ms, redials, errors}'

offsets() {
  local args=()
  for p in $(seq 0 $((partitions - 1))); do
    args+=(-t "$topic:$p:-1")
  done
  kcat -b "$KAFKA_BROKERS" -Q "${args[@]}" 2>/dev/null | awk '{s += $NF} END {print s + 0}'
}

end=0
got=0
for _ in $(seq 1 180); do
  end=$(offsets)
  got=$(psql -tAc "SELECT coalesce(sum(next_offset), 0) FROM game.projector_offsets")
  [ "$end" = "$got" ] && break
  sleep 1
done
echo "$(date +%T) projector at $got of $end journal records"

report=$(psql -v ON_ERROR_STOP=1 -tA -f /opt/axon/toolbox/verify.sql)
echo "$report"
echo "$report" | jq -e '.sessions_with_acknowledged_moves_missing == 0 and .sessions_with_moves_missing_entirely == 0 and .completed_and_matching_db == .completed_by_client' >/dev/null
