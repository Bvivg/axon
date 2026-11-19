#!/usr/bin/env bash
set -euo pipefail
export XDG_RUNTIME_DIR=/tmp/axon-pulse
mkdir -p "$XDG_RUNTIME_DIR"
chmod 700 "$XDG_RUNTIME_DIR"
pulseaudio --start --exit-idle-time=-1 --log-target=file:/evidence/pulseaudio.log
pactl load-module module-null-sink sink_name=axon_spike >/dev/null
pactl set-default-sink axon_spike
install -m 644 /evidence/.secrets/ca.crt /usr/local/share/ca-certificates/axon-calls-spike.crt
update-ca-certificates >/dev/null
mkdir -p /root/.pki/nssdb
certutil -N --empty-password -d sql:/root/.pki/nssdb
certutil -A -d sql:/root/.pki/nssdb -n axon-calls-spike -t 'C,,' -i /evidence/.secrets/ca.crt
exec npx playwright test --config="${CALLS_PLAYWRIGHT_CONFIG:-playwright.calls-spike.config.ts}" "$@"
