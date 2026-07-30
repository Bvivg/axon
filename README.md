# Axon

A modular Go microservice monorepo with realtime features and multiple clients:
authentication, chat, turn-based games and calling, behind a single Connect
gateway, with a Next.js web client and a SwiftUI app.

> **Status: foundation.** The shared packages, container stack, code generation
> and CI are in place. Services land on top of them stage by stage, starting
> with auth and the gateway.

## Layout

```
core/
  go.work                Go workspace
  shared/
    pkg/                 packages every service uses
      correlation/       request-scoped correlation IDs on context
      logger/            slog wrapper with the mandatory log fields
      config/            env loading, fail-fast, secret redaction
      health/            /healthz (liveness) and /readyz (readiness)
      middleware/        HTTP middleware and Connect interceptors
      postgres/          pgx pool + readiness check
      redis/             Redis client + readiness check
    proto/               service contracts, the source of truth for all clients
  services/              one Go module per service
  deploy/                compose stack, tooling and image definitions
ui/web/                  Next.js client
docs/                    architecture notes and diagrams
```

## Getting started

Requires Docker. Go is only needed for formatting and unit tests; everything
else — Postgres, Redis, `buf`, `golangci-lint` — runs in a container.

```sh
make env     # create core/deploy/.env from the example
make up      # start the stack and wait for it to report healthy
make check   # gofmt, go vet and unit tests
make ps      # what is running
make down    # stop, keeping data (make reset also drops volumes)
```

`make` on its own lists every target.

Host ports default to 55432 (Postgres) and 56379 (Redis) rather than the usual
5432/6379, which are nearly always taken by another project. Override them in
`core/deploy/.env`; traffic between containers is unaffected.

## Working on it

```sh
make proto        # regenerate Go and TypeScript from the contracts
make lint         # golangci-lint, containerised and version-pinned
make test         # unit tests with the race detector
make fmt          # gofmt
```

Generated code is not committed: `core/shared/gen/` and `ui/web/src/gen/` are
git-ignored and rebuilt with `make proto`.

## Design notes

A few decisions worth stating up front; the reasoning is expanded in
`docs/architecture.md` as the services arrive.

- **One contract, three protocols.** Connect serves gRPC, gRPC-Web and plain
  JSON from the same definition, so the web client needs no proxy and `curl`
  still works.
- **Postgres is the source of truth.** Redis holds only what can be rebuilt from
  it — caches, presence, pub/sub fan-out — so losing Redis costs latency, not
  data.
- **Schema-level isolation.** Each service owns its own Postgres schema and
  migrations and never reaches across the boundary, so services stay
  independently deployable while the local stack runs one database.
- **The gateway is the trust boundary.** Authentication, CORS, rate limiting and
  input validation happen there. Resource-level authorization — is this user in
  this room, may this player move in this game — belongs to the service that
  owns the resource.
- **Correlation IDs from day one.** Every request carries one through logs and
  across service hops, which is also what distributed tracing will hang off
  later.

## Roadmap

| Stage | Scope |
|---|---|
| 0 | Foundation: shared packages, container stack, codegen, CI |
| 1 | Auth + gateway + web login — the first end-to-end path |
| 2 | Chat over WebSocket, with Kafka and Redis fan-out |
| 3 | Game engine and Tic-Tac-Toe |
| 4 | Chess, as proof that a new game needs no infrastructure changes |
| 5 | Calling via LiveKit |
| 6 | SwiftUI client for iOS and macOS |
| 7 | Architecture docs, Grafana dashboards, tracing |
