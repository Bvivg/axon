#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../../../.." && pwd)
k8s=$root/core/deploy/k8s
charts=$k8s/charts
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
catalog='https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'
converter=${OPENAPI2JSONSCHEMA:-https://raw.githubusercontent.com/yannh/kubeconform/v0.7.0/scripts/openapi2jsonschema.py}
python=${PYTHON:-python3}
services="auth chat game gateway rating web"
envs="kind preview"

for chart in $services; do
  helm dependency build "$charts/$chart" >/dev/null
done

helm lint "$charts"/* "$k8s/argocd/apps" --quiet
for env in $envs; do
  for chart in $services infra cdc secrets-bootstrap; do
    helm lint "$charts/$chart" --quiet --values "$k8s/values-$env.yaml"
  done
done

chaos_version=$(awk '/chaosMesh:/ {found = 1} found && /version:/ {print $2; exit}' "$k8s/argocd/apps/values.yaml")
mkdir -p "$work/schemas"
helm pull chaos-mesh --repo https://charts.chaos-mesh.org --version "$chaos_version" --untar --untardir "$work" >/dev/null
case "$converter" in
  http*) curl -fsSL -o "$work/openapi2jsonschema.py" "$converter" ;;
  *) cp "$converter" "$work/openapi2jsonschema.py" ;;
esac
(cd "$work/schemas" && "$python" "$work/openapi2jsonschema.py" "$work"/chaos-mesh/crds/*.yaml >/dev/null)

mkdir -p "$work/rendered"
for env in $envs; do
  namespace=axon
  [ "$env" = preview ] && namespace=axon-pr-1
  for chart in $services infra cdc secrets-bootstrap; do
    helm template "$chart" "$charts/$chart" --namespace "$namespace" --values "$k8s/values-$env.yaml" \
      >"$work/rendered/$env-$chart.yaml" 2>/dev/null
  done
  helm template "$namespace" "$charts/stack" --set env="$namespace" --set valueFile="values-$env.yaml" \
    >"$work/rendered/$env-stack.yaml"
done
helm template platform "$charts/platform" --namespace cert-manager >"$work/rendered/platform.yaml"
helm template root "$k8s/argocd/apps" --namespace argocd >"$work/rendered/apps.yaml"
cp "$k8s/argocd/root.yaml" "$work/rendered/root.yaml"
for file in "$k8s"/jobs/*.yaml "$k8s"/chaos/*.yaml; do
  cp "$file" "$work/rendered/$(basename "$(dirname "$file")")-$(basename "$file")"
done

kubeconform -strict -summary -output text \
  -schema-location default \
  -schema-location "$catalog" \
  -schema-location "$work/schemas/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json" \
  "$work/rendered"
