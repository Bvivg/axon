#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../../../.." && pwd)
cluster=${KIND_CLUSTER:-axon}
tag=${IMAGE_TAG:-local}
api_url=${WEB_API_BASE_URL:-http://localhost:28080}
images=${IMAGES:-auth chat game gateway rating web connect toolbox}

cd "$root"

if [ ! -d core/shared/gen/go ]; then
  echo "generated contracts are missing, run make proto first" >&2
  exit 1
fi

for image in $images; do
  echo "build axon/$image:$tag"
  case "$image" in
    chat)
      docker build -q -f core/deploy/Dockerfile.service --target runtime \
        --build-arg SERVICE=chat --build-arg RUNTIME_FLAVOR=media -t "axon/chat:$tag" core >/dev/null
      ;;
    auth | game | gateway | rating)
      docker build -q -f core/deploy/Dockerfile.service --target runtime \
        --build-arg SERVICE="$image" -t "axon/$image:$tag" core >/dev/null
      ;;
    web)
      docker build -q -f core/deploy/Dockerfile.web --target runtime \
        --build-arg NEXT_PUBLIC_API_BASE_URL="$api_url" -t "axon/web:$tag" ui/web >/dev/null
      ;;
    connect)
      docker build -q -f core/deploy/k8s/Dockerfile.connect -t "axon/connect:$tag" core/deploy/k8s >/dev/null
      ;;
    toolbox)
      docker build -q -f core/deploy/k8s/Dockerfile.toolbox -t "axon/toolbox:$tag" core >/dev/null
      ;;
    *)
      echo "unknown image $image" >&2
      exit 2
      ;;
  esac
done

if kind get clusters 2>/dev/null | grep -qx "$cluster"; then
  for image in $images; do
    kind load docker-image --name "$cluster" "axon/$image:$tag" >/dev/null
    echo "loaded axon/$image:$tag into $cluster"
  done
fi
