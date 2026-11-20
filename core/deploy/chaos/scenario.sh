#!/usr/bin/env bash
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
hold=${HOLD:-30}
case "${1:-}" in
  postgres-latency) "$here/toxic.sh" latency postgres "${LATENCY_MS:-300}" ;;
  redis-outage) "$here/toxic.sh" outage redis ;;
  restart)
    [ -n "${2:-}" ] || { echo "usage: $0 restart <container>" >&2; exit 2; }
    docker stop --time 30 "$2" >/dev/null
    echo "$2 stopped"
    sleep "${DOWN:-5}"
    docker start "$2" >/dev/null
    echo "$2 started"
    exit 0
    ;;
  *) echo "usage: $0 postgres-latency | redis-outage | restart <container>" >&2; exit 2 ;;
esac
trap '"$here/toxic.sh" reset' EXIT
sleep "$hold"
