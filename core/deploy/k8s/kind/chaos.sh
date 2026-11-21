#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
k8s=$(cd "$here/.." && pwd)
context=kind-${KIND_CLUSTER:-axon}
namespace=${NAMESPACE:-axon}
chaos=${CHAOS:-1}
delay=${CHAOS_DELAY:-20}
log=$(mktemp)

k() { kubectl --context "$context" -n "$namespace" "$@"; }

k delete workflow.chaos-mesh.org axon-resilience --ignore-not-found --wait=true >/dev/null

watch_replicas() {
  while :; do
    printf '%s %s %s\n' "$(date +%T)" \
      "$(k get deploy game -o jsonpath='{.status.readyReplicas}/{.spec.replicas}')" \
      "$(k get hpa game -o jsonpath='{.status.currentMetrics[0].pods.current.averageValue}' 2>/dev/null)" >>"$log"
    sleep 5
  done
}
watch_replicas &
watcher=$!
trap 'kill $watcher 2>/dev/null || true; rm -f "$log"' EXIT

if [ "$chaos" = 1 ]; then
  (
    sleep "$delay"
    k apply -f "$k8s/chaos/workflow.yaml" >/dev/null
    echo "$(date +%T) chaos workflow axon-resilience started"
  ) &
fi

status=0
"$here/run-job.sh" chaos-load "$k8s/jobs/load.yaml" load || status=$?

if [ "$chaos" = 1 ]; then
  for _ in $(seq 1 180); do
    done=$(k get workflow.chaos-mesh.org axon-resilience -o jsonpath='{.status.conditions[?(@.type=="Accomplished")].status}' 2>/dev/null || true)
    [ "$done" = True ] && break
    sleep 5
  done
  echo "workflow axon-resilience accomplished=${done:-unknown}"
  k get workflownodes.chaos-mesh.org -l chaos-mesh.org/workflow=axon-resilience \
    -o custom-columns=NODE:.spec.templateName,TYPE:.spec.type,STARTED:.spec.startTime,DEADLINE:.spec.deadline --sort-by=.spec.startTime 2>/dev/null || true
  [ "$done" = True ] || status=1
fi

if [ "${WAIT_SCALE_DOWN:-0}" = 1 ]; then
  min=$(k get hpa game -o jsonpath='{.spec.minReplicas}')
  for _ in $(seq 1 60); do
    [ "$(k get deploy game -o jsonpath='{.spec.replicas}')" = "$min" ] && break
    sleep 5
  done
  sleep 10
fi

echo "game replicas over time (ready/desired, active sessions per pod):"
awk '{ if ($2 != prev) { print; prev = $2 } }' "$log"
max=$(awk '{ split($2, r, "/"); if (r[2] > m) m = r[2] } END { print m + 0 }' "$log")
echo "max desired game replicas: $max"
exit "$status"
