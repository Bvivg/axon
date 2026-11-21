# 0004. Tournaments as an orchestrated saga over the outbox

- Status: accepted, 2026-11-20 (phase 7c)
- Code: [`core/services/tournament`](../../core/services/tournament/README.md), [`core/services/game/internal/tournament`](../../core/services/game/internal/tournament), [`core/shared/pkg/tournamentid`](../../core/shared/pkg/tournamentid)

## Context

A tournament ties dozens of games into one business transaction that lasts minutes or hours, spans two services (tournament and game), must survive restarts of either and cannot keep state in memory. Games already publish `game.finished.v1` through the outbox and Debezium ([ADR 0003](0003-outbox-cdc-debezium.md)), so the delivery primitives exist.

## Decision

- **Orchestration, not choreography.** A new `tournament` service owns the saga. `internal/saga` is a pure state machine (`registration → seeding → running → finished | cancelled`); `internal/orchestrator` runs it.
- **One step, one transaction.** Lock the tournament row `FOR UPDATE`, run the pure function, write the changed rows and the resulting events to `tournament.outbox` in the same transaction.
- **Commands to game are events:** `tournament.round_started.v1`, `tournament.forfeit.v1`, `tournament.cancelled.v1`. Game consumes them in group `game-tournaments`.
- **Results come only from `game.finished.v1`**, which carries `tournament_id` and `plies`. A win in fewer than 2 plies is a forfeit.
- **Idempotency by construction.** Tournament id is UUIDv5(creator, request); board session id is UUIDv5(tournament, round, board), recomputed and checked by game; event ids are UUIDv5 of subject, kind and number with `ON CONFLICT DO NOTHING`; `processed_events` dedupes results; a decided match ignores a second result.
- **Timeouts are rows.** No-show deadlines live in `tournament.deadlines`. A sweeper (default every 1 s) re-reads each due row `FOR UPDATE` under the tournament lock and fires it once. The same sweeper re-steps tournaments stuck in `seeding` or with a finished but unadvanced round.
- **Compensation.** Cancel before start returns registrations. Admin cancel during play marks open matches `aborted`; game records the tournament in `game.cancelled_tournaments`, cancels the sessions without `game.finished`, and ignores late commands for it.

## Alternatives rejected

- Choreography between game and tournament: the round and bracket logic would be spread over two services with no single place to read the state.
- In-memory timers for no-shows: lost on restart.
- Rematches on a drawn knockout game: tic-tac-toe could draw forever, so the higher seed advances.
- Synchronous rating lookup in game at registration: tournament keeps its own projection of `rating.changed.v1`.
- Float scores in Swiss: integer half-points.

## Consequences

- Every step pays the CDC path: in the e2e run a result took 2.8 s on average from game end to commit, mostly CDC on a loaded VM.
- Swiss pairing has unit tests only and was never run end to end. Admin cancel during play is tested in integration and server unit tests, not through the gateway.
- The web client polls every 2 s instead of receiving pushes.
- The "Open game" link appears only once game has created the session.

## Evidence

- [Phase 7c results](../../.claude/plans/platform-phase-7c-tournaments.md#результаты-2026-11-20): 18 deliveries → 7 applied and 11 duplicates across an orchestrator restart, exactly 3 `round_started` and 1 `finished`; two sweepers fired one forfeit; e2e with a no-show finished in 39.4 s. Summary in [benchmarks](../benchmarks.md#tournament-saga).
- `core/services/tournament/integration/saga_test.go`, `core/services/game/integration/tournament_test.go`, `make test-e2e-tournament`.
