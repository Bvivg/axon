#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

make env
make jwt-key
make up

set -a
source core/deploy/.env
set +a

echo
echo "axon is up:"
echo "  web       http://localhost:${WEB_HOST_PORT:-3003}"
echo "  gateway   http://localhost:${GATEWAY_HOST_PORT:-18080}"
echo "  minio     http://localhost:${MINIO_CONSOLE_HOST_PORT:-59001}"
echo
echo "make logs   to follow all services"
echo "make down   to stop (keeps data)"
