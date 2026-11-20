#!/usr/bin/env bash
set -euo pipefail
api=${TOXIPROXY_URL:-http://127.0.0.1:18474}
usage() { echo "usage: $0 latency <proxy> <ms> | outage <proxy> | reset" >&2; exit 2; }
case "${1:-}" in
  latency)
    [ $# -eq 3 ] || usage
    curl -fsS -X POST "$api/proxies/$2/toxics" -H 'content-type: application/json' \
      -d "{\"name\":\"latency_$2\",\"type\":\"latency\",\"stream\":\"downstream\",\"attributes\":{\"latency\":$3,\"jitter\":0}}" >/dev/null
    echo "latency $3 ms on $2"
    ;;
  outage)
    [ $# -eq 2 ] || usage
    curl -fsS -X POST "$api/proxies/$2" -H 'content-type: application/json' -d '{"enabled":false}' >/dev/null
    echo "$2 cut"
    ;;
  reset)
    curl -fsS -X POST "$api/reset" >/dev/null
    echo "all proxies enabled, toxics removed"
    ;;
  *) usage ;;
esac
