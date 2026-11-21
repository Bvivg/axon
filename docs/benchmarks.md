# Benchmarks and verification results

Every number on this page was measured once, on a developer laptop, and written into the results section of a phase plan the same day. Each row links to that section. The plans are in Russian; this page is the English summary. Nothing here is re-run in CI, so treat these as recorded results, not as guarantees checked on every commit.

Phase plans: [1](../.claude/plans/platform-phase-1-game-event-sourcing.md), [1b](../.claude/plans/platform-phase-1b-game-performance.md), [2](../.claude/plans/platform-phase-2-outbox-cdc.md), [3](../.claude/plans/platform-phase-3-scale-resilience.md), [4](../.claude/plans/platform-phase-4-observability.md), [5](../.claude/plans/platform-phase-5-security.md), [6](../.claude/plans/platform-phase-6-k8s-gitops.md), [7c](../.claude/plans/platform-phase-7c-tournaments.md). Design context: [ADRs](adr/README.md).

## Hardware

| Run | Recorded environment |
|---|---|
| Phase 1 baseline | A laptop; Docker Compose ([phase 1 context](../.claude/plans/platform-phase-1-game-event-sourcing.md#контекст)) |
| Phase 1b A/B | 1 Kafka broker, 1 game instance, 10 CPU cores for Docker ([phase 1b results](../.claude/plans/platform-phase-1b-game-performance.md#результаты-2026-11-20)) |
| Phase 3 chaos | `axon-ha-check`: base + `ha` + `chaos`, two replicas of gateway, auth, chat and game, 3 Kafka brokers, Envoy, Postgres replica, PgBouncer, Toxiproxy ([phase 3 results](../.claude/plans/platform-phase-3-scale-resilience.md#результаты-2026-11-20)) |
| Phase 6 Kubernetes | kind: 1 control plane and 3 workers on the same laptop; the Docker VM reached a load average of 128 on 10 CPUs while other stacks ran in parallel ([phase 6 results](../.claude/plans/platform-phase-6-k8s-gitops.md#результаты-2026-11-20)) |

CPU model and memory were not recorded.

## Load generator

`core/services/game/cmd/loadgen` plays complete chess, Quoridor and tic-tac-toe games over the game WebSocket, straight to the game service inside the Docker network. It signs in with the `X-Axon-Test-User` header, which exists only when `GAME_TEST_HOOKS=true`, so the gateway and the TLS front are not part of these numbers. It writes per-match results, and each run is reconciled against `game.moves` in the read model after the projector has caught up with the journal.

Flags: `-target`, `-users`, `-legends`, `-out`, `-matches`, `-concurrency`, `-pace` (minimum gap between commands on one socket), `-timeout`, `-seed`, `-redials`.

| Run | Command |
|---|---|
| Phase 1 A/B | `-matches 20000 -concurrency 300`, one seed for both images; the full invocation was not recorded ([phase 1 A/B](../.claude/plans/platform-phase-1-game-event-sourcing.md#ab-на-тестовом-входе-20-ноября-2026)) |
| Phase 1b A/B | `loadgen -target http://game:8083 -matches 6000 -concurrency 1000 -pace 260ms -seed 11`, once per image ([phase 1b results](../.claude/plans/platform-phase-1b-game-performance.md#результаты-2026-11-20)) |
| Phase 3 chaos | `loadgen -target http://game:8083 -matches 4000 -concurrency 300 -pace 260ms -seed 21`, with [`core/deploy/chaos/scenario.sh`](../core/deploy/chaos/scenario.sh) running at the same time ([phase 3 results](../.claude/plans/platform-phase-3-scale-resilience.md#результаты-2026-11-20)) |
| Phase 6 HPA | `CHAOS=0 WAIT_SCALE_DOWN=1 make kind-chaos` (defaults in [`chaos-load.sh`](../core/deploy/k8s/toolbox/chaos-load.sh): 2000 matches, 150 in flight, pace 260ms, seed 21) |
| Phase 6 chaos | `make kind-chaos`, and `JOB_ENV='{"MATCHES":"4000"}' make kind-chaos` for the phase 3 size |

## Load and latency

### Phase 1: Postgres as source of truth vs Kafka journal

500 games without pauses, same games in both runs, driven by the earlier `load500` client through the gateway rather than `loadgen`. "After" bypassed the TLS front and used two game instances. Source: [phase 1 results](../.claude/plans/platform-phase-1-game-event-sourcing.md#результат-20-ноября-2026).

| | Before: Postgres | After: Kafka journal, game ×2 |
|---|---|---|
| Run time | 41.4 s | 33.3 s |
| Moves applied per second | ~955 | ~1190 |
| Move latency p50 / p95 / p99 | 152 / 205 / 234 ms | 39 / 196 / 301 ms |
| Failures | 0 | 0 |
| Peak CPU, cores | game 2.2, Postgres 1.9 | game 1.7 + 1.5, Kafka 1.5 / 1.3 / 1.3, Postgres 1.3 |

The "after" p95/p99 tail is a client artefact: replies became faster than the limit of 4 commands per second per socket, so commands were rejected with `RATE_LIMITED` and retried after 260 ms.

20,000 matches split evenly over the three games, 300 in flight, one seed, 10,000 users, game ×2 in both designs, test login. Source: [phase 1 A/B](../.claude/plans/platform-phase-1-game-event-sourcing.md#ab-на-тестовом-входе-20-ноября-2026).

| | Postgres as source of truth | Kafka journal |
|---|---|---|
| Time | 850 s | 448 s |
| Moves per second | 803 | 1524 |
| Move latency p50 / p95 / p99 | 277 / 767 / 1144 ms | 77 / 357 / 604 ms |
| Read model reconciliation | 683,639 = 683,639 | 683,639 = 683,639 |
| Read model lag after the peak | 0 | 167 s |
| CPU, cores, average | game 2.7, Postgres 1.9 | game 4.5, Kafka 2.5, Postgres 0.6 |

The bottleneck moved from Postgres to game: every event carried a full snapshot, the producer did not batch, the projector wrote row by row. That is phase 1b.

### Phase 1b: compact facts, batched projector, Quoridor bitsets

6,000 matches of three games, 1,000 in flight, 260 ms pause between moves, seed 11, 1 broker, 1 game. Source: [phase 1b results](../.claude/plans/platform-phase-1b-game-performance.md#результаты-2026-11-20).

| | Phase 1 | Phase 1b |
|---|---|---|
| Moves per second | 3498 | 3683 (client pause ceiling ~3800) |
| Move latency p50 / p95 / p99 | 23 / 184 / 335 ms | 2 / 8 / 19 ms |
| game CPU | 4.10 cores | 2.12 cores |
| game cores per 1,000 moves/s | 1.17 | 0.58 |
| Projector lag after the peak | 10 s | 1 s |
| Read model reconciliation | 204,840 = 204,840 | 204,840 = 204,840 |

- Without the Quoridor change, 1b used as much CPU as phase 1 (1.51 vs 1.28 cores per 1,000 moves/s). Under load 42% of game CPU went to `quoridor.View → LegalMoves`. With walls as two `uint64` bitmasks and a full BFS only for walls that cut a current shortest path, `LegalMoves` went from 597 to 31 µs and from 8,613 to 142 allocations ([phase 1b decision](../.claude/plans/platform-phase-1b-game-performance.md#решение)).
- Producer linger 0 vs 1 ms: p99 380 vs 426 ms, but game CPU +10%; 1 ms was kept.

### Phase 4: with full tracing on

300 games in flight, 260 ms pause, 100% trace sampling through the observability overlay: p50 2 ms, p95 8–16 ms, p99 27–40 ms. The first run right after a game restart spiked p99 to ~400 ms (partition claims and a cold projector cache); repeats did not. Source: [phase 4 results](../.claude/plans/platform-phase-4-observability.md#результаты-2026-11-20).

### Phase 6: in Kubernetes

| Run | Matches | Moves | Moves/s | p99 | Source |
|---|---|---|---|---|---|
| HPA, no chaos | 2000, 150 in flight | 67,985 | 753 | 280 ms | [phase 6](../.claude/plans/platform-phase-6-k8s-gitops.md#результаты-2026-11-20) |
| Chaos workflow | 4000 | 136,441 | 575 | 754 ms | [phase 6](../.claude/plans/platform-phase-6-k8s-gitops.md#результаты-2026-11-20) |

These ran on an overloaded host (see Hardware) and are not comparable with the Compose numbers.

## Chaos and failover

Reconciliation in every row: each move the client saw acknowledged must be in `game.moves` after the projector caught up.

| Scenario | Result | Source |
|---|---|---|
| `docker kill` of the owner of 12 partitions, 8 s into 50 chess games (3 brokers, game ×2) | 50/50 games finished; acknowledged moves 3937 = 3937 in the read model; failover pause ~11 s (group session timeout 10 s) | [phase 1](../.claude/plans/platform-phase-1-game-event-sourcing.md#результат-20-ноября-2026) |
| Phase 3 sequence under 4,000 games: Postgres +300 ms for 20 s, Redis down 10 s, restart game-1, chat-1, gateway-1, auth-1 | See the table below | [phase 3](../.claude/plans/platform-phase-3-scale-resilience.md#результаты-2026-11-20) |
| Planned handover when game-1 stops / returns | Second replica took 12 partitions in 0.3 s; rebalance on return in 0.5 s | [phase 3](../.claude/plans/platform-phase-3-scale-resilience.md#результаты-2026-11-20) |
| gateway restart behind Envoy (5 s drain) | 0 upstream connection errors, 0 retries | [phase 3](../.claude/plans/platform-phase-3-scale-resilience.md#результаты-2026-11-20) |
| Chaos Mesh workflow in kind, 14 steps (Postgres latency, Redis loss, pod kills with 30 s grace, Kafka broker kill), 2000 games | `Accomplished`; completed 1990 = 1990 matching the DB; 0 sessions with acknowledged moves missing; 10 games cut at the client (`4001`), 1974 redials; p99 767 ms | [phase 6](../.claude/plans/platform-phase-6-k8s-gitops.md#результаты-2026-11-20) |
| Same workflow, 4000 games | `Accomplished`; 3999 = 3999; 0 lost moves; 1 client timeout; HPA 2 → 4 → 2; projector caught up with all 291,043 journal records | [phase 6](../.claude/plans/platform-phase-6-k8s-gitops.md#результаты-2026-11-20) |
| CloudNativePG primary deleted | New primary after 3 min 26 s (180 s of it is CNPG smart shutdown); Debezium connector stayed RUNNING; slot on the new primary has `failover=true`; rating e2e green afterwards | [phase 6](../.claude/plans/platform-phase-6-k8s-gitops.md#результаты-2026-11-20) |

Phase 3, four consecutive runs of the same scenario:

| Run | Finished and matching the DB | Lost acknowledged moves | Client failures |
|---|---|---|---|
| 1: game cancelled requests on SIGTERM, loadgen did not reconnect | — | 0 | 1651 |
| 2: graceful shutdown in game, loadgen reconnects | 3762 / 4000 | 0 | 238 timeouts (a move lost with its socket was not resent) |
| 3: loadgen resends the move | 3885 / 4000 | 0 | 115 × `4001 moved` |
| 4: leases on a dedicated pool | 3999 / 4000 | 0 | 1 first connection during the game restart |

Run 3 exposed ownership flapping: under +300 ms Postgres latency, lease renewals queued behind the projector on the shared 10-connection pool, overran their 2 s budget and expired (`game_partition_revocations_total{reason="lease_expired"}` = 12). With a 4-connection lease pool, run 4 had 0 renewal errors.

## Outbox, CDC and rating exactly-once

Runner: [`core/services/rating/e2e/chain_test.go`](../core/services/rating/e2e/chain_test.go) via [`core/deploy/rating-e2e.sh`](../core/deploy/rating-e2e.sh); `make test-rating-e2e`. Source: [phase 2 results](../.claude/plans/platform-phase-2-outbox-cdc.md#результаты-2026-11-20).

| Check | Result |
|---|---|
| One rated game through the gateway | `game.outbox` row 8 ms after the last move; Kafka value byte-equal to the outbox payload; ratings 1362.31 / 1037.69, RD 290.32; `game.ratings` updated 1.7–3.4 s after the last move |
| Debezium Connect stopped 60 s under load | 50 rated games in 65 s, 40 of them finished while the slot was inactive; connector RUNNING 23 s after start; `processed_events` caught up 30 s after the last game; 50 sessions each exactly once; `rating.changes` 100 rows, `game.rating_changes` 100, no duplicates; `game.ratings` = `rating.ratings` for all 10 players; 50 records in `game.finished.v1` for 50 events |
| `rating -rebuild` from the topic | exit 0; 83 events, 83 applied, 0 duplicates, 26 players, equal to `rating.ratings` and the leaderboard, 130 ms |
| Fresh repeat (`make test-rating-e2e` on a new project) | 50 games, 45 with Connect stopped; caught up in 22 s; rebuild over 51 events, exit 0 |

## Tournament saga

Source: [phase 7c results](../.claude/plans/platform-phase-7c-tournaments.md#результаты-2026-11-20).

| Check | Result |
|---|---|
| Integration: 8-player bracket, orchestrator restarted after two boards of round 1 | `finished` in 6.6 s; 18 `game.finished.v1` deliveries (duplicates with the same and with a new event id, a result for an already decided board, deliveries while the orchestrator was down): 7 applied, 11 duplicates; exactly 3 `round_started` and 1 `finished` in the outbox |
| Integration: no-show deadline of 2 s with two sweepers | Exactly one `forfeit` command, at most 9 ms after the deadline |
| Integration, game side: four `round_started` deliveries and a `forfeit` before `round_started` | 3 sessions and one forfeit (`game.finished` with `plies=0`) |
| E2E `make test-e2e-tournament`, 8 players through the gateway, no-show timeout 30 s | Start to `finished` in 39.4 s; repeated `CreateTournament` returned the same tournament; seed 8 did not show up and lost by forfeit 5.5 s after the deadline; winner seed 1; `tournament_rounds_total 3`, `tournament_forfeits_total 1` |
| Saga step latency (`tournament_saga_step_seconds`, event to commit) | seed 3.5 ms; record mean 2.8 s over 7 steps (mostly CDC on a loaded VM); advance mean 0.8 s over 3; forfeit 0.25 s |

## Security

Source: [phase 5 results](../.claude/plans/platform-phase-5-security.md#результаты-2026-11-20) and, for kind, [phase 6 results](../.claude/plans/platform-phase-6-k8s-gitops.md#результаты-2026-11-20).

| Check | Result |
|---|---|
| JWKS rotation under load (`make test-e2e-security`): interval 45 s, lead 15 s, token TTL 5 min, 8 threads, 3 min | 81,310 `GetMe` calls, 0 rejections, 4 signing keys |
| Rotation simulation | 6 hours at a 1-hour interval; every live token verified at every step |
| Concurrent rotation | Four auth instances under the advisory lock create one key per interval (integration test) |
| mTLS rejection (Compose) | Plain HTTP, TLS with no client certificate and an `intruder` certificate from the same CA rejected by auth, chat, game and rating |
| mTLS rejection (kind, `foreign-ca` step) | 4/4 rejected (`tlsv1 alert unknown ca`) by auth, chat, game, rating |
| Certificate renewal | auth presented a renewed certificate without a restart |
| Image scan | Trivy on auth, chat, gateway, game, rating images: 0 fixable HIGH/CRITICAL |

## Kubernetes

Source: [phase 6 results](../.claude/plans/platform-phase-6-k8s-gitops.md#результаты-2026-11-20).

| Check | Result |
|---|---|
| Argo CD | 19 applications Synced/Healthy |
| `make kind-down && make kind-up` from scratch | exit 0 in 44 min (2644 s); cluster and cached images 3 min, the rest cold pulls of operator and infrastructure images |
| `make kind-e2e` (24 s) | auth 62/0, chat 26/0, rating 2/0 (+1 skip), security 17/0 (+2 skip), foreign CA 4/4 rejected |
| HPA on `game_active_sessions` (min 2, max 6, target 50 per pod) | 2 → 3 after ~30 s at 73.5 active games per pod; back to 2 ~30 s after the load; 2000 = 2000 reconciled, 0 lost moves |
| PDB `minAvailable: 1`, two game nodes drained at once | One eviction refused (`would violate the pod's disruption budget`) and retried once the replacement was Ready (~4 s); at least 1 Ready game pod throughout; drains took 9 s and 4 s |
| Pull-request preview | `axon-pr-21` and 9 child apps Synced/Healthy in 6 min 31 s; removing the label deleted them in 6 min 8 s |
| `make k8s-validate` | 161 manifests, 0 errors |

kind-e2e, HPA, PDB, chaos and preview ran on the previous cluster with the same charts; after the from-scratch rebuild they were not re-run.

## Observability

| Check | Result | Source |
|---|---|---|
| One trace per move | `gateway.ws move → game.command move → journal.append → projector.apply` under one trace id, started in the browser | [phase 4](../.claude/plans/platform-phase-4-observability.md#результаты-2026-11-20) |
| Alert rules | 8 rules under promtool; the burn-rate test fires fast and slow alerts at 20% slow commands and stays silent at 0.5% | [phase 4](../.claude/plans/platform-phase-4-observability.md#результаты-2026-11-20) |

## Not measured

- No run compares the HA overlay with the base stack under the same load.
- The mTLS overlay was never load-tested together with observability or SFU, because those combinations do not work (see [core/deploy/README.md](../core/deploy/README.md#which-overlays-combine)).
- The raw CSV and scripts of these runs were kept outside the repository and are not published.
