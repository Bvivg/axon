#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
k8s=$(cd "$here/.." && pwd)
context=kind-${KIND_CLUSTER:-axon}
namespace=${NAMESPACE:-axon}
job=$1
manifest=$2
container=$3

k() { kubectl --context "$context" -n "$namespace" "$@"; }

k apply -f "$k8s/jobs/toolbox.yaml" >/dev/null
k wait --for=condition=Ready certificate/toolbox-mtls certificate/intruder-mtls --timeout=180s >/dev/null
k delete job "$job" --ignore-not-found --wait=true >/dev/null
if [ -n "${JOB_ENV:-}" ]; then
  k apply -f "$manifest" --dry-run=client -o json | jq --arg c "$container" --argjson env "$JOB_ENV" '
    .spec.template.spec.containers |= map(if .name == $c then .env = ((.env | map(select(.name as $n | ($env | has($n)) | not))) + ($env | to_entries | map({name: .key, value: .value}))) else . end)' | k apply -f - >/dev/null
else
  k apply -f "$manifest" >/dev/null
fi
k wait --for=condition=PodReadyToStartContainers pod -l "job-name=$job" --timeout=300s >/dev/null 2>&1 || true
for _ in $(seq 1 120); do
  phase=$(k get pod -l "job-name=$job" -o jsonpath='{.items[0].status.containerStatuses[?(@.name=="'"$container"'")].state}' 2>/dev/null || true)
  case "$phase" in *running* | *terminated*) break ;; esac
  sleep 2
done
k logs -f "job/$job" -c "$container" || true
for _ in $(seq 1 60); do
  succeeded=$(k get job "$job" -o jsonpath='{.status.succeeded}')
  failed=$(k get job "$job" -o jsonpath='{.status.failed}')
  [ "${succeeded:-0}" -ge 1 ] && exit 0
  [ "${failed:-0}" -ge 1 ] && exit 1
  sleep 2
done
echo "job $job did not finish" >&2
exit 1
