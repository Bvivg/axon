# 0006. mTLS between services and rotating JWT signing keys

- Status: accepted, 2026-11-20 (phase 5; cert-manager variant in phase 6)
- Code: [`core/shared/pkg/mtls`](../../core/shared/pkg/mtls/mtls.go), [`core/services/auth/internal/keyring`](../../core/services/auth/internal/keyring), [`core/shared/pkg/authn`](../../core/shared/pkg/authn), [`core/deploy/mtls`](../../core/deploy/mtls)

## Context

Internal calls (gateway to auth, chat, game; chat to auth; game to game; JWKS downloads) went over plain HTTP inside the `app` network, so any container on it could call any service. The JWT signing key was a single static key; changing it meant editing `.env` and restarting. Access tokens are verified locally in every service from JWKS, so a key change has to reach every verifier before the first token signed with it.

## Decision

### mTLS

- `shared/pkg/mtls` loads the certificate, key and CA from files (`MTLS_CERT_FILE`, `MTLS_KEY_FILE`, `MTLS_CA_FILE`) and reloads them when they change (`MTLS_RELOAD_INTERVAL`). Servers require and verify a client certificate, accept TLS 1.3 only, and allow only the names in `MTLS_ALLOWED_CLIENTS`. Without `MTLS_*` a service runs plain HTTP, as in local development.
- Allow-lists: auth accepts gateway, chat, game; chat accepts gateway; game accepts gateway and game; rating accepts gateway.
- Health, readiness and metrics move to a separate admin port, so probes and scrapes do not need a certificate.
- Compose: `step-ca` and a `certs` job issue a 1-hour certificate per service into its own volume and renew it with `step ca renew --daemon`. Kubernetes: cert-manager `Certificate` per service (ECDSA, 1 h, renew 20 min before expiry) from a self-signed `ClusterIssuer`.
- Short lifetime plus file reload is the rotation; no restarts.

### JWKS rotation

- Keys live in `auth.signing_keys`, private keys sealed with AES-GCM under `JWT_KEY_ENCRYPTION_KEY`.
- Every `JWT_ROTATION_INTERVAL`, one auth instance, under `pg_advisory_xact_lock`, creates a key with `activates_at = now + JWT_ROTATION_LEAD`. It is published in JWKS immediately but signs only after activation: **publish before sign**.
- Default lead is 20 min, longer than the slowest verifier's JWKS refresh (chat, 15 min).
- The previous key retires `access TTL + 1 min` after the new key activates, so every token it signed has expired.
- The decision logic (`keyring.Due`, `Successor`, `Set`) is pure and unit-tested; `JWT_ROTATION_INTERVAL=0` keeps static keys.

## Alternatives rejected

- Relying on the `app` network zone alone: any container attached to it could call any service.
- Long-lived certificates: short lifetimes with file reload make rotation the normal state instead of an event.
- One static signing key replaced by editing `.env` and restarting (the previous state): tokens signed by a new key fail on verifiers that have not refreshed JWKS yet.

## Consequences

- mTLS is an overlay (`docker-compose.mtls.yml`), not the default Compose stack. In kind it is on.
- It does not compose with the observability overlay (Prometheus scrapes game on 8083) or with the SFU overlay (LiveKit webhooks reach chat without a certificate), so the full production combination was never run as one Compose stack.
- step-ca's minimum certificate lifetime is 5 minutes, so the e2e uses 6-minute certificates.
- Kafka is plaintext with no ACLs.
- Cosign signing and GHCR publishing run only on push to `main` and had not run when phase 5 was recorded.

## Evidence

- [Phase 5 results](../../.claude/plans/platform-phase-5-security.md#результаты-2026-11-20): plain HTTP, no certificate and an `intruder` certificate rejected by auth, chat, game and rating; rotation under load with 81,310 `GetMe` calls, 0 rejections, 4 keys; 6-hour simulation.
- [Phase 6 results](../../.claude/plans/platform-phase-6-k8s-gitops.md#результаты-2026-11-20): a foreign CA rejected 4/4 in kind.
- `make test-e2e-security`; summary in [benchmarks](../benchmarks.md#security).
