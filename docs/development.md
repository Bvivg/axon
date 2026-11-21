# Development

There are two ways to run Axon. The local launcher is for day-to-day work; the
Docker stack is what reviews and end-to-end tests run against.

## Local launcher

Requires Node.js 24 (`nvm use`), Go, Docker and ffmpeg/ffprobe on the device.
The launcher also detects Node.js 24+ already installed through nvm when the
shell still uses an older version. The workspace selects its Go toolchain.

```sh
npm run start:dev
```

This single command prepares the development environment, installs native web
dependencies, generates missing contracts, applies migrations, and starts
Next.js plus the Go services directly on the device. Shared modules compile with
the services. The web client supports hot reload; restart the command after
changing Go code.

The launcher starts only infrastructure in Docker: Postgres, Redis, Kafka and
MinIO, plus configured calling infrastructure. It preserves an existing Axon
Compose stack and does not stop infrastructure on exit. Ctrl+C stops all local
application processes, including child processes. An occupied port or a failed
service aborts startup and stops the processes started by this command.

Defaults: web `http://localhost:3000`, gateway `http://localhost:28080`,
auth/chat/game/rating/analysis/tournament `28081`–`28086`, admin ports
`29090`–`29092` for gateway/auth/chat and `29094`–`29096` for
rating/analysis/tournament. The analysis service starts only when Stockfish is installed
or `STOCKFISH_PATH` is set in `.env.local`.
Logs are in `.local/dev/`. Local development uses the configured development
database; migrations apply to that database.

Configuration loads from `core/deploy/.env.example`, `core/deploy/.env`, root
`.env.local`, then the shell environment. Missing `.env` and JWT signing keys
are created automatically. Keep secrets in ignored env files. Calling feature
flags and credentials retain their configured values.

Override `DEV_WEB_PORT`, `DEV_GATEWAY_PORT`, `DEV_AUTH_PORT`, `DEV_CHAT_PORT`,
`DEV_GAME_PORT`, `DEV_RATING_PORT`, `DEV_ANALYSIS_PORT`, `DEV_TOURNAMENT_PORT`
or the matching `DEV_*_ADMIN_PORT` in root `.env.local` to change application ports.
Infrastructure uses the host ports from `core/deploy/.env`. For infrastructure
already running outside Compose, set `AXON_DEV_INFRA=external` and configure
service DSNs, `REDIS_ADDR`, `DEV_KAFKA_BROKERS`, `DEV_S3_ENDPOINT`,
`DEV_S3_PUBLIC_URL`, and, if SFU is enabled, `DEV_LIVEKIT_URL`.

## Docker stack

`startup.sh` and `make up` start the full Compose stack (every service, Kafka,
Debezium, Apicurio, MinIO). They are not the local launcher; keep their ports
separate from local dev processes. Images copy generated contracts, so run
`make proto` first on a fresh clone.

```sh
make proto   # generate Go and TypeScript from the contracts, in a container
make env     # create core/deploy/.env from the example
make up      # start the stack and wait for it to report healthy
make ps      # what is running
make logs    # follow all services
make down    # stop, keeping data (make reset also drops volumes)
```

`make` on its own lists every target.

Host ports default to 55432 (Postgres) and 56379 (Redis) rather than the usual
5432/6379, which are nearly always taken by another project. Override them in
`core/deploy/.env`; traffic between containers is unaffected.

Heavier modes (HA, mTLS, observability, chaos, backup, calls) are Compose
overlays on top of the base file; see [`core/deploy/README.md`](../core/deploy/README.md).

## Kubernetes

```sh
make kind-up       # kind cluster, images, Argo CD, wait until every application is Synced and Healthy
make kind-e2e      # auth, chat, rating and mTLS e2e suites inside the cluster
make kind-chaos    # game load under the Chaos Mesh workflow with move reconciliation
make k8s-validate  # helm lint and kubeconform over the rendered manifests
make kind-down
```

Design and results: [phase 6 plan](../.claude/plans/platform-phase-6-k8s-gitops.md).

## Working on it

```sh
make check        # gofmt, go vet and unit tests: everything CI runs without Docker
make lint         # golangci-lint, containerised and version-pinned
make test         # unit tests with the race detector
make fmt          # gofmt
make web-check    # web lint, type-check, unit tests and build, in a container
```

Generated code is not committed: `core/shared/gen/` and `ui/web/src/gen/` are
git-ignored and rebuilt with `make proto`.

## Test suites

| Target | What it runs |
|---|---|
| `make test` | Go unit tests with `-race` across all modules |
| `make test-integration` | testcontainers Postgres and Kafka integration tests |
| `make test-e2e` | auth and chat e2e against the shipping images, also in CI |
| `make test-web-e2e` | Playwright browser suite against the full stack, also in CI |
| `make test-rating-e2e` | rated games through gateway, CDC and rating, with a Debezium outage and a rebuild |
| `make test-e2e-security` | mTLS rejection and JWT key rotation on the mTLS overlay |
| `make test-e2e-game` | spectators and replay through the gateway |
| `make test-e2e-analysis` | a chess game through CDC to a Stockfish report |
| `make test-e2e-tournament` | an eight-player bracket with a no-show through the saga |

Each one-off suite runs in its own Compose project and removes it afterwards.
