#!/usr/bin/env bash
set -uo pipefail

logs=${E2E_LOGS:-/tmp/e2e}
suites=${E2E_SUITES:-auth chat rating security foreign-ca}
mkdir -p "$logs"
failed=0

wait_for() {
  for _ in $(seq 1 60); do
    if curl -s -o /dev/null "$1"; then
      return 0
    fi
    sleep 1
  done
  echo "timed out waiting for $1" >&2
  return 1
}

suite() {
  local name=$1 dir=$2
  shift 2
  local started=$SECONDS
  if (cd "$dir" && "$@" -test.v -test.count=1 -test.timeout=20m >"$logs/$name.log" 2>&1); then
    result=PASS
  else
    result=FAIL
    failed=1
  fi
  printf '%-10s %s passed=%s failed=%s skipped=%s seconds=%s\n' "$name" "$result" \
    "$(grep -c -- '--- PASS' "$logs/$name.log")" "$(grep -c -- '--- FAIL' "$logs/$name.log")" \
    "$(grep -c -- '--- SKIP' "$logs/$name.log")" $((SECONDS - started))
  if [ "$result" = FAIL ]; then
    grep -B2 -A20 -- '--- FAIL' "$logs/$name.log" | tail -80
  fi
}

foreign_ca() {
  local dir
  dir=$(mktemp -d)
  openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 1 \
    -subj /CN=foreign-ca -keyout "$dir/ca.key" -out "$dir/ca.crt" >/dev/null 2>&1
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
    -subj /CN=gateway -addext subjectAltName=DNS:gateway -keyout "$dir/tls.key" -out "$dir/tls.csr" >/dev/null 2>&1
  openssl x509 -req -in "$dir/tls.csr" -CA "$dir/ca.crt" -CAkey "$dir/ca.key" -CAcreateserial -days 1 \
    -copy_extensions copy -out "$dir/tls.crt" >/dev/null 2>&1
  local started=$SECONDS refused=0 total=0
  for target in auth:8081:/axon.auth.v1.AuthService/GetMe chat:8082:/axon.chat.v1.ChatService/ListRooms \
    game:8083:/axon.game.v1.GameService/ListSessions rating:8084:/axon.rating.v1.RatingService/GetLeaderboard; do
    IFS=: read -r host port path <<<"$target"
    total=$((total + 1))
    if curl -sS --max-time 5 --cacert /certs/intruder/ca.crt --cert "$dir/tls.crt" --key "$dir/tls.key" \
      -H 'Content-Type: application/json' -d '{}' -o /dev/null "https://$host:$port$path" 2>"$logs/foreign-$host.err"; then
      echo "foreign-ca $host accepted a certificate from a foreign CA" >&2
    else
      refused=$((refused + 1))
      echo "foreign-ca $host refused: $(tr -d '\n' <"$logs/foreign-$host.err")" >>"$logs/foreign-ca.log"
    fi
  done
  if [ "$refused" -eq "$total" ]; then result=PASS; else result=FAIL; failed=1; fi
  printf '%-10s %s refused=%s/%s seconds=%s\n' foreign-ca "$result" "$refused" "$total" $((SECONDS - started))
  cat "$logs/foreign-ca.log"
  rm -rf "$dir"
}

wait_for "$GATEWAY_ADMIN_URL/readyz"
wait_for "http://127.0.0.1:18081/"

for name in $suites; do
  case "$name" in
    auth) suite auth /opt/axon auth-e2e.test ;;
    chat) suite chat /opt/axon/chat chat-e2e.test ;;
    rating) suite rating /opt/axon rating-e2e.test ;;
    security) suite security /opt/axon security-e2e.test ;;
    foreign-ca) foreign_ca ;;
    *) echo "unknown suite $name" >&2; failed=1 ;;
  esac
done

exit "$failed"
