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

Requires Node.js 24 (`nvm use`), Go, Docker and ffmpeg/ffprobe on the device.
The launcher also detects Node.js 24+ already installed through nvm when the
shell still uses an older version. The workspace selects its Go toolchain.

```sh
npm run start:dev
```

This single command prepares the development environment, installs native web
dependencies, generates missing contracts, applies migrations, and starts
Next.js plus all implemented Go services (`auth`, `chat`, `game`, `gateway`)
directly on the device. Shared modules compile with the services. The SwiftUI
client has not been implemented yet. The web client supports hot reload; restart
the command after changing Go code.

The launcher starts only infrastructure in Docker: Postgres, Redis, Kafka and
MinIO, plus configured calling infrastructure. It preserves an existing Axon
Compose stack and does not stop infrastructure on exit. Ctrl+C stops all local
application processes, including child processes. An occupied port or a failed
service aborts startup and stops the processes started by this command.

Defaults: web `http://localhost:3000`, gateway `http://localhost:28080`,
auth/chat/game `28081`/`28082`/`28083`, admin ports `29091`/`29092`/`29090`.
Logs are in `.local/dev/`. Local development uses the configured development
database; migrations apply to that database.

Configuration loads from `core/deploy/.env.example`, `core/deploy/.env`, root
`.env.local`, then the shell environment. Missing `.env` and JWT signing keys
are created automatically. Keep secrets in ignored env files. Calling feature
flags and credentials retain their configured values.

Override `DEV_WEB_PORT`, `DEV_GATEWAY_PORT`, `DEV_AUTH_PORT`, `DEV_CHAT_PORT`,
`DEV_GAME_PORT`, `DEV_AUTH_ADMIN_PORT`, `DEV_CHAT_ADMIN_PORT`, or
`DEV_GATEWAY_ADMIN_PORT` in root `.env.local` to change application ports.
Infrastructure uses the host ports from `core/deploy/.env`. For infrastructure
already running outside Compose, set `AXON_DEV_INFRA=external` and configure
service DSNs, `REDIS_ADDR`, `DEV_KAFKA_BROKERS`, `DEV_S3_ENDPOINT`,
`DEV_S3_PUBLIC_URL`, and, if SFU is enabled, `DEV_LIVEKIT_URL`.

Before handing changes over for review, build and verify the application in
Docker Compose. `startup.sh` and `make up` start that Docker stack; they are
not the local application dev launcher. Keep its ports separate from local
dev processes. The following commands manage the Docker stack:

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
