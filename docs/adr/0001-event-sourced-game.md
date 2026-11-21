# 0001. Live games on a Kafka journal, Postgres as the read model

- Status: accepted, 2026-11-20 (phase 1), revised in phase 1b
- Code: [`core/services/game/internal/journal`](../../core/services/game/internal/journal), [`owner`](../../core/services/game/internal/owner/README.md), [`projection`](../../core/services/game/internal/projection), [`cluster`](../../core/services/game/internal/cluster)

## Context

Every game command was one Postgres transaction: `SELECT … FOR UPDATE` on the session, change it, `UPDATE game.sessions`, `INSERT game.moves`, `pg_notify`. Postgres was the source of truth and game was stateless. On a laptop this topped out at about 1,000 moves/s with p50 152 ms for 500 concurrent games, with game and Postgres both busy.

The project is a portfolio. The platform plan states the goal as demonstrating distributed-system decisions, each proven by a test or a measurement, not serving real load. This ADR should be read with that in mind: the baseline was already enough for any realistic number of players.

## Decision

- **Journal.** Topic `game.session-events.v1`, 24 partitions. The partition is `fnv32a(session_id) % 24`, set explicitly with a manual partitioner so the router, producer and projector agree. The producer is idempotent, `acks=all`, lz4, linger 1 ms. Records are `JournalRecord`: a session fact, or a `PartitionClaimed` fencing marker.
- **Single writer in memory.** The instance that owns a partition holds its live sessions in memory, decides each command with pure functions (`session.Rules`), appends the fact and acknowledges only after the append. Ownership is [ADR 0002](0002-ownership-leases-fencing.md).
- **Compact facts (phase 1b).** A move is `MoveApplied` with a short `MoveOutcome` (status, turn, remaining clock), not a snapshot. Rare facts carry a full snapshot. `journal.Fold` replays a move with the same rules code and fails with `ErrDiverged` if the result does not match the recorded outcome.
- **Projector.** Consumer group `game-projector` folds each poll into Postgres in one transaction per batch: `game.sessions`, `game.moves`, the `game.projector_offsets` row and any outbox rows. Upserts are guarded by `revision < EXCLUDED.revision`. The transaction runs with `synchronous_commit = off`, which is safe because the offset is in the same transaction and replaying from the journal is idempotent.
- **Reads** (lists, history, ratings) come from the read model; reads of one live session go to the owner.

## What is the system of record

For a **live** game the journal is: a move is acknowledged once it is in Kafka, before Postgres has it. For anything older, it is not, and this should be said plainly:

- On `main`, `journal.NewKafka` creates the topic with only `min.insync.replicas` set, and the Strimzi `KafkaTopic` in [`charts/infra`](../../core/deploy/k8s/charts/infra/templates/kafka.yaml) sets the same. The topic therefore inherits the broker's default retention, and neither the Compose stack nor the Strimzi `Kafka` resource overrides it. Broker topic auto-creation is also on.
- Since phase 1b the replay base for a session is its read-model row, not a snapshot in the log. Once old segments are deleted, the read model is the only full copy of finished games and cannot be rebuilt from offset 0.

So today the journal acts as a replicated write-ahead log for live games, and Postgres is the durable record. For the journal to be the system of record in the full sense, the journal topic must be retained indefinitely; see the hardening PR (`fix/game-owner-hardening`).

## Alternatives rejected

- Keep Postgres row locks as the write path: measured at 803 moves/s and p50 277 ms in the A/B, against 1,524 moves/s and p50 77 ms on the journal.
- A full session snapshot in every event (phase 1): cost O(n) per move for chess, O(n²) per game; replaced in 1b.
- Periodic snapshots in a `game.snapshots` table (the original plan): unnecessary once the read model row, written with its offset in one transaction, serves as the replay base.
- An actor goroutine with a mailbox per game: a cell with a mutex and a timer gives the same ordering with less code.
- A routing map published by the group leader into Redis: replaced by `game.owners` in Postgres with `LISTEN game_owner_changed`.

## Consequences

- Throughput and latency improved, availability did not. Postgres is still on the availability path: leases live in `game.owners`, and if renewal fails for longer than the lease span every owner gives up its partitions. Phase 3 hit this as ownership flapping under 300 ms of Postgres latency and fixed it with a dedicated lease pool ([benchmarks](../benchmarks.md#chaos-and-failover)).
- Two coordination planes: Kafka group assignment decides who should own a partition, Postgres decides whether that owner may write. [ADR 0002](0002-ownership-leases-fencing.md) explains why.
- The read model lags the journal. In the phase 1 A/B it was 167 s behind after the peak; after phase 1b, 1 s.
- `game.finished.v1` is not derived from the journal but from the read model through the outbox ([ADR 0003](0003-outbox-cdc-debezium.md)).
- On `main`, an append that fails ambiguously leaves the owner's in-memory cell unchanged; the fix is in the same hardening work ([owner guide](../../core/services/game/internal/owner/README.md#known-gaps-on-main)).
- Owners keep every session of an owned partition in memory until the partition moves.

## Evidence

- [Phase 1 results and A/B](../../.claude/plans/platform-phase-1-game-event-sourcing.md#результат-20-ноября-2026), [phase 1b results](../../.claude/plans/platform-phase-1b-game-performance.md#результаты-2026-11-20), summarized in [benchmarks](../benchmarks.md#load-and-latency).
- `docker kill` of an owner mid-way through 50 games: 3,937 = 3,937 acknowledged moves in the read model.
- Tests: `journal/fold_test.go` (replay equals the original byte for byte on random games of all three kinds), `owner/owner_test.go`, `projection/postgres_integration_test.go`.
