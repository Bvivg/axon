# Architecture

Axon is seven Go services behind one gateway. Game state is event-sourced on a
Kafka journal; every cross-service effect goes through a transactional outbox
and Debezium. Numbers quoted here come from the results sections of the phase
plans in [`.claude/plans/`](../.claude/plans/).

## Container view

```mermaid
flowchart TB
  subgraph clients["Clients"]
    web["Web client, Next.js"]
    ios["iOS and macOS client, SwiftUI, in review"]
  end
  subgraph edge["Edge"]
    gw["gateway: Connect and WebSocket proxy, JWT via JWKS, CORS, rate limits"]
    eredis[("edge Redis")]
  end
  subgraph services["Services"]
    auth["auth: accounts, OAuth, roles, rotating JWKS"]
    chat["chat: messages, media, search, Calls module"]
    game["game: partition owners, projector, matchmaking"]
    rating["rating: Glicko-2, leaderboard"]
    analysis["analysis: Stockfish worker"]
    tournament["tournament: saga orchestrator"]
  end
  subgraph data["Data"]
    pg[("Postgres, schema per service")]
    redis[("Redis")]
    kafka[("Kafka")]
    minio[("MinIO")]
    debezium["Debezium Connect, outbox router"]
    apicurio["Apicurio Registry"]
  end
  subgraph media["Media"]
    coturn["coturn"]
    livekit["LiveKit SFU"]
  end
  web -->|"Connect, WebSocket"| gw
  ios -.->|"Connect, WebSocket"| gw
  web -.->|"WebRTC media"| coturn
  web -.->|"WebRTC media"| livekit
  gw --> eredis
  gw --> auth & chat & game & analysis & tournament
  tournament -->|"roles"| auth
  auth & chat & game & rating & analysis & tournament --> pg
  auth & chat & rating --> redis
  chat --> minio
  chat -->|"rooms, admission"| livekit
  game -->|"journal, leases"| kafka
  debezium -->|"logical replication of outbox tables"| pg
  debezium -->|"domain events"| kafka
  kafka -->|"game.finished.v1"| rating & analysis & tournament
  kafka -->|"rating.changed.v1, tournament commands"| game
```

The gateway is the trust boundary: it verifies JWTs locally against JWKS,
applies CORS and GCRA rate limits in its own Redis, and turns the refresh token
into an `httpOnly` cookie. Resource-level authorization stays in the service
that owns the resource. Services verify tokens themselves and, in the mTLS
overlay, accept only the client certificates on their allow-list.

## A move, end to end

```mermaid
sequenceDiagram
  autonumber
  participant C as Client
  participant G as Gateway WebSocket
  participant O as Game partition owner
  participant J as Kafka journal
  participant P as Projector
  participant DB as Postgres
  participant D as Debezium
  participant E as game.finished.v1
  participant S as rating, analysis, tournament
  C->>G: move frame with traceparent
  G->>O: game replica forwards to the owner of the session partition
  O->>O: engine validates the move, lease still held
  O->>J: append SessionRecorded with the owner generation, acks=all
  J-->>O: offset
  O->>O: lease still held after the append
  O-->>C: new snapshot to players and spectators
  J->>P: records batched per partition
  P->>DB: upsert read model and offset in one transaction
  Note over P,DB: a finished game also writes its outbox row in this transaction
  D->>DB: read the outbox through logical replication
  D->>E: EventRouter with event_id and event_type headers
  E->>S: consume, skip ids already in processed_events
```

The owner acknowledges a move only after the journal append succeeds while its
lease is valid, so an acknowledged move is in the log. The projector upserts
with `WHERE revision < EXCLUDED.revision` and commits its Kafka offset in the
same Postgres transaction, so replays are harmless. `game.finished.v1` comes
from the outbox rather than the journal because it must be atomic with the read
model settlement. One trace ID covers `gateway.ws move → game.command →
journal.append → projector.apply`
([phase 4](../.claude/plans/platform-phase-4-observability.md)).

Load results: 803 → 1,524 moves/s against the old row-lock path, then p99
335 → 19 ms after compact events and a batched projector
([phase 1](../.claude/plans/platform-phase-1-game-event-sourcing.md),
[phase 1b](../.claude/plans/platform-phase-1b-game-performance.md)).

## Partition ownership

Sessions hash to the 24 partitions of `game.session-events.v1`. Kafka consumer
group assignment decides which replica should own a partition; a lease row in
`game.owners` decides whether it may write.

```mermaid
stateDiagram-v2
  [*] --> Claiming: consumer group assigns the partition
  Claiming --> CatchUp: generation bumped in game.owners, old lease waited out, PartitionClaimed marker appended
  Claiming --> [*]: claim failed
  CatchUp --> Serving: journal replayed from the projected offset to the marker, filtered by generation
  Serving --> Serving: lease renewed every third of its span
  Serving --> Draining: rebalance revokes the partition
  Serving --> Draining: lease taken by a newer generation
  Serving --> Draining: lease expired, also when it lapses during an append
  Draining --> [*]: in-flight commands finish, lease released, sockets get 4001 moved
  note right of Serving
    Every append carries the owner generation.
    The projector and the next owner drop events
    from an older generation, so a zombie owner
    cannot overwrite the new one.
  end note
```

The fencing epoch is `game.owners.generation` in Postgres rather than the Kafka
group generation, which resets when an empty group is deleted. Lease renewals
run on a dedicated connection pool: on the shared pool, 300 ms of Postgres
latency made leases expire and ownership flap, which the phase 3 chaos run
caught and the dedicated pool fixed
([phase 3](../.claude/plans/platform-phase-3-scale-resilience.md)).

## Tournament saga

```mermaid
stateDiagram-v2
  state "running, round in play" as playing
  state "running, round complete" as roundDone
  [*] --> registration: CreateTournament, idempotent by request_id
  registration --> cancelled: creator or admin cancels, registrations returned
  registration --> seeding: StartTournament
  seeding --> playing: seed, pair round 1, emit round_started, write no-show deadlines
  playing --> playing: game.finished.v1 recorded once per session
  playing --> playing: overdue deadline swept with SKIP LOCKED, forfeit command emitted
  playing --> roundDone: last board of the round decided
  roundDone --> playing: pair the next round
  roundDone --> finished: last round done, emit tournament.finished.v1
  playing --> cancelled: admin cancels, unfinished matches aborted
  finished --> [*]
  cancelled --> [*]
```

Each step is one transaction: the tournament row is locked `FOR UPDATE`, a pure
state machine computes the next state and its events, and state plus outbox rows
commit together. Event ids are UUIDv5 of tournament, round and board, and the
outbox insert is `ON CONFLICT DO NOTHING`, so a replayed step publishes nothing
twice. Game derives the same session id from the event and rejects mismatches.
Deadlines live in a table, not in memory timers
([phase 7c](../.claude/plans/platform-phase-7c-tournaments.md)).

## Network zones

The Compose project uses one network per responsibility, and each container
joins only the zones it needs. Stores are reachable only from their owners and
migration jobs; the gateway, web and media servers are not on `data`.

```mermaid
flowchart LR
  edgeN{{"edge"}}
  appN{{"app"}}
  dataN{{"data"}}
  mediaN{{"media"}}
  stateN{{"edge-state"}}
  obsN{{"obs, observability overlay"}}
  web["web"] --- edgeN
  gateway["gateway"] --- edgeN & appN & mediaN & stateN
  auth["auth"] --- edgeN & appN & dataN
  minio["MinIO"] --- edgeN & dataN
  chat["chat"] --- appN & dataN & mediaN
  game["game"] & rating["rating"] & analysis["analysis"] & tournament["tournament"] --- appN & dataN
  stores["Postgres, Redis, Kafka, Debezium, Apicurio, migration jobs"] --- dataN
  eredis["edge Redis"] --- stateN
  calls["LiveKit, coturn"] --- mediaN
  telemetry["OTel Collector, Prometheus, Tempo, Loki, Alloy, Pyroscope, Grafana"] --- obsN
```

A `lan` zone carries DNS and checks for devices on the local network in the calls
overlays. Services join `obs` only in `docker-compose.observability.yml`.
Overlays and their combinations: [`core/deploy/README.md`](../core/deploy/README.md).

## Kubernetes and GitOps

```mermaid
flowchart TB
  up["make kind-up"] --> kind["kind cluster: control plane and 3 workers in zones a, b, c"]
  up --> argocd["Argo CD"]
  git[("Git branch on GitHub")] --> root
  argocd --> root["root Application"]
  root --> apps["apps chart, app of apps"]
  apps --> operators["operators: cert-manager, CloudNativePG, Strimzi, Redis Operator, Prometheus Operator, prometheus-adapter, Chaos Mesh"]
  apps --> platform["platform: internal CA and ClusterIssuer"]
  apps --> stack["stack chart, environment axon"]
  apps --> preview["ApplicationSet: preview namespace per labelled PR"]
  stack --> w0["wave 0: secrets bootstrap"]
  w0 --> w1["wave 1: Postgres x3, Kafka x3, Redis with sentinel, MinIO, Apicurio"]
  w1 --> w2["wave 2: auth, rating"]
  w2 --> w3["wave 3: chat, game"]
  w3 --> w4["wave 4: gateway, CDC connector"]
  w4 --> w5["wave 5: web"]
  w3 -.-> hpa["HPA on game_active_sessions through prometheus-adapter, 2 to 6 pods"]
```

Services share one library chart: Deployment, Service, PDB, HPA,
ServiceMonitor, a cert-manager `Certificate` for mTLS, PreSync migrations and
probes on the admin port. Game is a Deployment rather than a StatefulSet because
leases, not pod identity, decide ownership. Results: 19 applications
Synced/Healthy; HPA 2 → 3 under 150 parallel games and back to 2; a two-node
drain held by the PDB; the Chaos Mesh workflow with 4,000 games lost 0
acknowledged moves; CDC survived a Postgres failover
([phase 6](../.claude/plans/platform-phase-6-k8s-gitops.md)).

## Decisions

Full records: [docs/adr/README.md](adr/README.md). Benchmarks:
[docs/benchmarks.md](benchmarks.md). Compose overlays:
[core/deploy/README.md](../core/deploy/README.md).

- **Connect** — one contract served as gRPC, gRPC-Web and JSON, so the web
  client needs no proxy and `buf breaking` guards every client at once.
- **Schema per service** — each service owns a Postgres schema and login role
  with a pinned `search_path`, so services stay independently deployable on one
  database.
- **Kafka journal for game writes** — a move is accepted when the partition
  owner appends it, and Postgres is a projection; in Compose the journal keeps
  the broker's default retention, so the read model is the long-term record
  ([ADR 0001](adr/0001-event-sourced-game.md),
  [ADR 0002](adr/0002-ownership-leases-fencing.md)).
- **Redis is not a source of truth** — it holds rate limits, presence, fan-out
  and the leaderboard, all rebuildable, so losing it costs latency, not data.
- **Outbox and CDC** — events commit with the state change and Debezium
  publishes them, with deterministic ids and `processed_events` for
  exactly-once effects ([ADR 0003](adr/0003-outbox-cdc-debezium.md)).
- **Tournaments as a saga** — a long business transaction over game events with
  table-backed deadlines and idempotent steps
  ([ADR 0004](adr/0004-tournament-saga.md)).
- **Calls inside Chat (ADR-1)** — one deployable for messaging and call
  signaling, with `axon.calls.v1` as the extraction boundary
  ([ADR 0005](adr/0005-calls-inside-chat.md), [overview](calls.md)).
- **mTLS and JWKS rotation** — short-lived service certificates with
  allow-lists, and signing keys published before they sign, so rotation needs no
  downtime ([ADR 0006](adr/0006-mtls-and-jwks-rotation.md)).
- **No comments in code** — the why lives in plans, ADRs and commit messages
  ([ADR 0007](adr/0007-no-comments-in-code.md)).
