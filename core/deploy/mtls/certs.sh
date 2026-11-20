#!/bin/sh
set -eu

root=/ca/certs/root_ca.crt
password=$(mktemp)
printf '%s' "$STEP_CA_PASSWORD" > "$password"

issue() {
  dir="/certs/$1"
  mkdir -p "$dir"
  step ca certificate "$1" "$dir/tls.crt.next" "$dir/tls.key.next" \
    --san "$1" --not-after "$MTLS_CERT_LIFETIME" \
    --provisioner "$STEP_PROVISIONER" --provisioner-password-file "$password" \
    --ca-url "$STEP_CA_URL" --root "$root" --force >/dev/null 2>&1
  cp "$root" "$dir/ca.crt"
  mv "$dir/tls.key.next" "$dir/tls.key"
  mv "$dir/tls.crt.next" "$dir/tls.crt"
  chown -R "$MTLS_OWNER" "$dir"
  chmod 0400 "$dir/tls.key"
  echo "issued $1"
}

renew() {
  dir="/certs/$1"
  [ -s "$dir/tls.crt" ] || return 1
  if step certificate needs-renewal "$dir/tls.crt" --expires-in "$MTLS_RENEW_BEFORE" >/dev/null 2>&1; then
    issue "$1"
  fi
}

for id in $MTLS_IDENTITIES; do
  issue "$id"
done
touch /certs/.ready

while true; do
  sleep "$MTLS_RENEW_CHECK"
  for id in $MTLS_IDENTITIES; do
    renew "$id" || issue "$id" || echo "could not issue $id"
  done
done
