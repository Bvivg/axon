#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../../.." && pwd)
cluster=${KIND_CLUSTER:-axon}
context=kind-$cluster
revision=${ARGO_REVISION:-$(git -C "$root" rev-parse --abbrev-ref HEAD)}
argocd_version=${ARGOCD_CHART_VERSION:-10.18.2}
deadline=$((SECONDS + ${KIND_UP_TIMEOUT:-5400}))

k() { kubectl --context "$context" "$@"; }

if ! git -C "$root" ls-remote --exit-code --heads origin "$revision" >/dev/null; then
  echo "branch $revision is not on origin; push it first, Argo CD syncs from GitHub" >&2
  exit 1
fi

if ! kind get clusters 2>/dev/null | grep -qx "$cluster"; then
  kind create cluster --name "$cluster" --config "$here/cluster.yaml" --wait 180s
fi

KIND_CLUSTER=$cluster "$here/images.sh"

helm upgrade --install argocd argo-cd --repo https://argoproj.github.io/argo-helm --version "$argocd_version" \
  --kube-context "$context" --namespace argocd --create-namespace \
  --values "$root/core/deploy/k8s/argocd/values.yaml" --wait --timeout 10m >/dev/null
echo "argo cd $argocd_version is running"

sed -e "s|targetRevision: main|targetRevision: $revision|" -e "s|revision: main|revision: $revision|" \
  "$root/core/deploy/k8s/argocd/root.yaml" | k apply -f - >/dev/null
echo "root application points at $revision"

expected=${KIND_EXPECTED_APPS:-19}
last=""
while :; do
  report=$(k -n argocd get applications.argoproj.io -o json | jq -r '
    [.items[] | select(.metadata.name | startswith("axon-pr-") | not)
      | {name: .metadata.name, sync: (.status.sync.status // "Unknown"), health: (.status.health.status // "Unknown")}] as $apps
    | "\($apps | length) \([$apps[] | select(.sync == "Synced" and .health == "Healthy")] | length) \([$apps[] | select(.sync != "Synced" or .health != "Healthy") | "\(.name)=\(.sync)/\(.health)"] | join(" "))"')
  read -r total ready pending <<<"$report"
  if [ "$total" -ge "$expected" ] && [ "$total" -eq "$ready" ]; then
    echo "all $total applications are Synced and Healthy"
    break
  fi
  if [ "$report" != "$last" ]; then
    echo "$(date +%T) $ready/$total ready ${pending:-}"
    last=$report
  fi
  if [ "$SECONDS" -ge "$deadline" ]; then
    echo "timed out waiting for applications" >&2
    k -n argocd get applications.argoproj.io >&2
    exit 1
  fi
  sleep 10
done

echo "gateway  http://localhost:28080"
echo "web      http://localhost:23003"
echo "argo cd  https://localhost:28443 (user admin, password in secret argocd/argocd-initial-admin-secret)"
