package projection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/session"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

const OwnerChannel = "game_owner_changed"

type Postgres struct {
	pool   *pgxpool.Pool
	leases *pgxpool.Pool
	rules  session.Rules

	mu    sync.Mutex
	cache map[uuid.UUID]session.Session
}

const cacheLimit = 50_000

type Option func(*Postgres)

func LeasesOn(pool *pgxpool.Pool) Option {
	return func(m *Postgres) {
		if pool != nil {
			m.leases = pool
		}
	}
}

func New(store *session.Store, rules session.Rules, options ...Option) *Postgres {
	m := &Postgres{pool: store.Pool(), leases: store.Pool(), rules: rules, cache: map[uuid.UUID]session.Session{}}
	for _, option := range options {
		option(m)
	}
	return m
}

func (m *Postgres) Forget() {
	m.mu.Lock()
	defer m.mu.Unlock()
	clear(m.cache)
}

func (m *Postgres) Claim(ctx context.Context, partition int32, ownerID, address string, ttl time.Duration) (owner.Lease, error) {
	var lease owner.Lease
	err := pgx.BeginFunc(ctx, m.leases, func(tx pgx.Tx) error {
		var waitMS int64
		err := tx.QueryRow(ctx, `SELECT CASE WHEN owner_id = $2 THEN 0
				ELSE GREATEST(0, CEIL(EXTRACT(EPOCH FROM (lease_until - clock_timestamp())) * 1000))::bigint END
			FROM game.owners WHERE partition=$1 FOR UPDATE`, partition, ownerID).Scan(&waitMS)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		lease.Wait = time.Duration(waitMS) * time.Millisecond
		if err := tx.QueryRow(ctx, `INSERT INTO game.owners (partition, owner_id, address, generation, lease_until)
			VALUES ($1,$2,$3,1, clock_timestamp() + make_interval(secs => $4))
			ON CONFLICT (partition) DO UPDATE SET owner_id=EXCLUDED.owner_id, address=EXCLUDED.address,
				generation=game.owners.generation+1, lease_until=EXCLUDED.lease_until, updated_at=clock_timestamp()
			RETURNING generation`, partition, ownerID, address, (lease.Wait + ttl).Seconds()).Scan(&lease.Generation); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `SELECT pg_notify($1,$2)`, OwnerChannel, fmt.Sprint(partition))
		return err
	})
	return lease, err
}

func (m *Postgres) Renew(ctx context.Context, ownerID string, held map[int32]int64, ttl time.Duration) ([]int32, error) {
	partitions := make([]int32, 0, len(held))
	generations := make([]int64, 0, len(held))
	for partition, generation := range held {
		partitions = append(partitions, partition)
		generations = append(generations, generation)
	}
	rows, err := m.leases.Query(ctx, `UPDATE game.owners o SET lease_until = clock_timestamp() + make_interval(secs => $4)
		FROM unnest($2::smallint[], $3::bigint[]) AS h(partition, generation)
		WHERE o.partition = h.partition AND o.generation = h.generation AND o.owner_id = $1
		RETURNING o.partition`, ownerID, partitions, generations, ttl.Seconds())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int32])
}

func (m *Postgres) Release(ctx context.Context, ownerID string, partition int32, generation int64) error {
	_, err := m.leases.Exec(ctx, `UPDATE game.owners SET lease_until = clock_timestamp()
		WHERE partition=$1 AND generation=$2 AND owner_id=$3`, partition, generation, ownerID)
	return err
}

func (m *Postgres) Projected(ctx context.Context, partition int32) (owner.Projection, error) {
	var out owner.Projection
	err := m.pool.QueryRow(ctx, `SELECT next_offset, claim_generation FROM game.projector_offsets WHERE partition=$1`, partition).Scan(&out.Offset, &out.Generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return owner.Projection{}, nil
	}
	return out, err
}

func (m *Postgres) Open(ctx context.Context, partition, partitions int32) ([]session.Session, error) {
	rows, err := m.pool.Query(ctx, `SELECT `+session.Columns+` FROM game.sessions
		WHERE (partition=$1 OR partition IS NULL) AND (state IS NULL OR state->>'status' = 'in_progress')`, partition)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var open []session.Session
	for rows.Next() {
		game, err := session.Scan(rows)
		if err != nil {
			return nil, err
		}
		if journal.PartitionOf(game.ID, partitions) == partition {
			open = append(open, game)
		}
	}
	return open, rows.Err()
}

func (m *Postgres) Load(ctx context.Context, id uuid.UUID) (session.Session, error) {
	return session.Scan(m.pool.QueryRow(ctx, `SELECT `+session.Columns+` FROM game.sessions WHERE id=$1`, id))
}

func (m *Postgres) MoveBefore(ctx context.Context, id uuid.UUID, ply int) (session.MoveRecord, bool, error) {
	var stateRaw, clockRaw, data []byte
	record := session.MoveRecord{Ply: ply}
	err := m.pool.QueryRow(ctx, `SELECT player_id, data, state_before, clock_before FROM game.moves WHERE session_id=$1 AND ply=$2`, id, ply).
		Scan(&record.Player, &data, &stateRaw, &clockRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return session.MoveRecord{}, false, nil
	}
	if err != nil {
		return session.MoveRecord{}, false, err
	}
	record.Data = data
	if len(stateRaw) > 0 && string(stateRaw) != "null" {
		if err := json.Unmarshal(stateRaw, &record.StateBefore); err != nil {
			return session.MoveRecord{}, false, err
		}
	}
	if len(clockRaw) > 0 && string(clockRaw) != "null" {
		if err := json.Unmarshal(clockRaw, &record.ClockBefore); err != nil {
			return session.MoveRecord{}, false, err
		}
	}
	return record, true, nil
}

func (m *Postgres) Moves(ctx context.Context, id uuid.UUID, below int) ([]session.MoveRecord, error) {
	rows, err := m.pool.Query(ctx, `SELECT ply, player_id, data FROM game.moves WHERE session_id=$1 AND ply<$2 ORDER BY ply`, id, below)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var moves []session.MoveRecord
	for rows.Next() {
		var move session.MoveRecord
		var data []byte
		if err := rows.Scan(&move.Ply, &move.Player, &data); err != nil {
			return nil, err
		}
		move.Data = data
		moves = append(moves, move)
	}
	return moves, rows.Err()
}

func (m *Postgres) Owners(ctx context.Context) (map[int32]string, error) {
	rows, err := m.leases.Query(ctx, `SELECT partition, address FROM game.owners`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int32]string{}
	for rows.Next() {
		var partition int32
		var address string
		if err := rows.Scan(&partition, &address); err != nil {
			return nil, err
		}
		out[partition] = address
	}
	return out, rows.Err()
}

type pending struct {
	game      session.Session
	exists    bool
	partition int32
	request   uuid.UUID
	write     bool
	cancelled bool
	notify    bool
	finished  *time.Time
	moves     map[int]played
	undone    map[int]bool
}

type played struct {
	session.MoveRecord
	At time.Time
}

func playedAt(at time.Time) time.Time { return at.UTC().Truncate(time.Microsecond) }

type batch struct {
	model *Postgres
	tx    pgx.Tx
	items map[uuid.UUID]*pending
	order []uuid.UUID
}

func (m *Postgres) Apply(ctx context.Context, batches map[int32][]journal.Entry) ([]uuid.UUID, error) {
	partitions := make([]int32, 0, len(batches))
	for partition := range batches {
		partitions = append(partitions, partition)
	}
	slices.Sort(partitions)
	var work *batch
	var notified []uuid.UUID
	err := pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		work = &batch{model: m, tx: tx, items: map[uuid.UUID]*pending{}}
		if _, err := tx.Exec(ctx, `SET LOCAL synchronous_commit = off`); err != nil {
			return err
		}
		offsets := make([]int32, 0, len(partitions))
		nexts := make([]int64, 0, len(partitions))
		generations := make([]int64, 0, len(partitions))
		for _, partition := range partitions {
			next, generation, err := lockOffset(ctx, tx, partition)
			if err != nil {
				return err
			}
			fence := journal.NewFence()
			fence.Seed(partition, generation)
			for _, entry := range batches[partition] {
				if entry.Offset < next {
					continue
				}
				next = entry.Offset + 1
				if !fence.Admit(entry) {
					continue
				}
				if err := work.fold(ctx, partition, *entry.Event); err != nil {
					return fmt.Errorf("projection: apply offset %d of partition %d: %w", entry.Offset, partition, err)
				}
			}
			offsets, nexts, generations = append(offsets, partition), append(nexts, next), append(generations, fence.Generation(partition))
		}
		var err error
		if notified, err = work.flush(ctx); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE game.projector_offsets o SET next_offset = n.next_offset, claim_generation = n.generation, updated_at = clock_timestamp()
			FROM unnest($1::smallint[], $2::bigint[], $3::bigint[]) AS n(partition, next_offset, generation)
			WHERE o.partition = n.partition`, offsets, nexts, generations)
		return err
	})
	if err != nil {
		return nil, err
	}
	m.remember(work)
	return notified, nil
}

func lockOffset(ctx context.Context, tx pgx.Tx, partition int32) (int64, int64, error) {
	var next, generation int64
	err := tx.QueryRow(ctx, `SELECT next_offset, claim_generation FROM game.projector_offsets WHERE partition=$1 FOR UPDATE`, partition).Scan(&next, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `INSERT INTO game.projector_offsets (partition, next_offset) VALUES ($1,0)`, partition)
	}
	return next, generation, err
}

func (m *Postgres) remember(work *batch) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.cache) > cacheLimit {
		clear(m.cache)
	}
	for _, id := range work.order {
		item := work.items[id]
		if !item.exists || item.game.Opponent != nil && item.game.State.Over() {
			delete(m.cache, id)
			continue
		}
		m.cache[id] = item.game
	}
}

func (m *Postgres) cached(id uuid.UUID) (session.Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	game, ok := m.cache[id]
	return game, ok
}

func (b *batch) item(ctx context.Context, id uuid.UUID, partition int32, reload bool) (*pending, error) {
	item := b.items[id]
	if item != nil && !reload {
		return item, nil
	}
	if item == nil {
		item = &pending{partition: partition, moves: map[int]played{}, undone: map[int]bool{}}
		b.items[id] = item
		b.order = append(b.order, id)
		if game, ok := b.model.cached(id); ok {
			item.game, item.exists = game, true
			return item, nil
		}
	}
	game, err := session.Scan(b.tx.QueryRow(ctx, `SELECT `+session.Columns+` FROM game.sessions WHERE id=$1`, id))
	switch {
	case errors.Is(err, session.ErrNotFound):
		item.game, item.exists = session.Session{}, false
	case err != nil:
		return nil, err
	default:
		item.game, item.exists = game, true
	}
	return item, nil
}

func (b *batch) fold(ctx context.Context, partition int32, event journal.Event) error {
	id := event.Session.ID
	item, err := b.item(ctx, id, partition, false)
	if err != nil {
		return err
	}
	base := item.base()
	folded, err := journal.Fold(b.model.rules, base, event)
	if errors.Is(err, journal.ErrGap) && !item.write {
		if item, err = b.item(ctx, id, partition, true); err != nil {
			return err
		}
		base = item.base()
		folded, err = journal.Fold(b.model.rules, base, event)
	}
	if err != nil {
		return err
	}
	if !folded.Applied {
		return nil
	}
	change := folded.Change
	item.game, item.partition = folded.Session, partition
	if change.Request != uuid.Nil {
		item.request = change.Request
	}
	if change.Fact == session.FactCancelled {
		item.exists, item.cancelled, item.write, item.notify = false, true, false, true
		return nil
	}
	item.exists, item.write = true, true
	switch change.Fact {
	case session.FactMoved:
		item.moves[change.Move.Ply] = played{MoveRecord: *change.Move, At: playedAt(change.At)}
	case session.FactUndone:
		delete(item.moves, change.UndonePly)
		item.undone[change.UndonePly] = true
		item.notify = true
	default:
		item.notify = true
	}
	if finished(item.game) && (base == nil || !finished(*base)) {
		at := item.game.UpdatedAt
		item.finished, item.notify = &at, true
	}
	return nil
}

func (item *pending) base() *session.Session {
	if !item.exists {
		return nil
	}
	game := item.game
	return &game
}

func (b *batch) flush(ctx context.Context) ([]uuid.UUID, error) {
	var cancelled, notified []uuid.UUID
	rows := &pgx.Batch{}
	var dropSessions []uuid.UUID
	var dropPlies []int32
	var dropKeep []*time.Time
	var sessions []uuid.UUID
	var plies []int32
	var players []uuid.UUID
	var data []string
	var clocks []*string
	var times []time.Time
	for _, id := range b.order {
		item := b.items[id]
		if item.notify {
			notified = append(notified, id)
		}
		if item.cancelled {
			cancelled = append(cancelled, id)
			continue
		}
		if !item.write {
			continue
		}
		if err := queueSession(rows, item); err != nil {
			return nil, err
		}
		for ply := range item.undone {
			var keep *time.Time
			if move, ok := item.moves[ply]; ok {
				keep = &move.At
			}
			dropSessions, dropPlies, dropKeep = append(dropSessions, id), append(dropPlies, int32(ply)), append(dropKeep, keep)
		}
		for ply, move := range item.moves {
			var clock *string
			if move.ClockBefore != nil {
				raw, err := json.Marshal(move.ClockBefore)
				if err != nil {
					return nil, err
				}
				value := string(raw)
				clock = &value
			}
			sessions, plies, players = append(sessions, id), append(plies, int32(ply)), append(players, move.Player)
			data, clocks, times = append(data, string(move.Data)), append(clocks, clock), append(times, move.At)
		}
	}
	if len(cancelled) > 0 {
		if _, err := b.tx.Exec(ctx, `DELETE FROM game.moves WHERE session_id = ANY($1)`, cancelled); err != nil {
			return nil, err
		}
		if _, err := b.tx.Exec(ctx, `DELETE FROM game.sessions s WHERE s.id = ANY($1)
			AND NOT EXISTS (SELECT 1 FROM game.rating_changes r WHERE r.session_id = s.id)`, cancelled); err != nil {
			return nil, err
		}
	}
	if rows.Len() > 0 {
		if err := b.tx.SendBatch(ctx, rows).Close(); err != nil {
			return nil, err
		}
	}
	if len(dropSessions) > 0 {
		if _, err := b.tx.Exec(ctx, `DELETE FROM game.moves m USING unnest($1::uuid[], $2::int[], $3::timestamptz[]) AS d(session_id, ply, keep)
			WHERE m.session_id = d.session_id AND m.ply = d.ply AND m.played_at IS DISTINCT FROM d.keep`, dropSessions, dropPlies, dropKeep); err != nil {
			return nil, err
		}
	}
	if len(sessions) > 0 {
		if _, err := b.tx.Exec(ctx, `INSERT INTO game.moves (session_id, ply, player_id, data, clock_before, played_at)
			SELECT s, p, pl, d::jsonb, c::jsonb, t FROM unnest($1::uuid[], $2::int[], $3::uuid[], $4::text[], $5::text[], $6::timestamptz[]) AS m(s, p, pl, d, c, t)
			ON CONFLICT (session_id, ply, played_at) DO UPDATE SET player_id=EXCLUDED.player_id, data=EXCLUDED.data,
				state_before=NULL, clock_before=EXCLUDED.clock_before`, sessions, plies, players, data, clocks, times); err != nil {
			return nil, err
		}
	}
	var events []outbox.Event
	for _, id := range b.order {
		item := b.items[id]
		if item.finished == nil || item.cancelled {
			continue
		}
		if event, ok := FinishedEvent(item.game, *item.finished); ok {
			events = append(events, event)
		}
	}
	if err := outbox.Write(ctx, b.tx, OutboxTable, events...); err != nil {
		return nil, fmt.Errorf("publish finished games: %w", err)
	}
	if len(notified) > 0 {
		ids := make([]string, len(notified))
		for i, id := range notified {
			ids[i] = id.String()
		}
		if _, err := b.tx.Exec(ctx, `SELECT pg_notify($1, id) FROM unnest($2::text[]) AS id`, session.ChangedChannel, ids); err != nil {
			return nil, err
		}
	}
	return notified, nil
}

func queueSession(rows *pgx.Batch, item *pending) error {
	game := item.game
	var state, clock any
	if game.Opponent != nil {
		raw, err := json.Marshal(game.State)
		if err != nil {
			return err
		}
		state = raw
	}
	if game.Clock != nil {
		raw, err := json.Marshal(game.Clock)
		if err != nil {
			return err
		}
		clock = raw
	}
	request := game.ID
	if item.request != uuid.Nil {
		request = item.request
	}
	rows.Queue(`INSERT INTO game.sessions (id, game, creator_id, opponent_id, request_id, state, updated_at, revision, clock,
			timed_out, rated, resigned, draw_offered_by, agreed_draw, partition)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (id) DO UPDATE SET opponent_id=EXCLUDED.opponent_id, state=EXCLUDED.state, updated_at=EXCLUDED.updated_at,
			revision=EXCLUDED.revision, clock=EXCLUDED.clock, timed_out=EXCLUDED.timed_out, rated=EXCLUDED.rated,
			resigned=EXCLUDED.resigned, draw_offered_by=EXCLUDED.draw_offered_by, agreed_draw=EXCLUDED.agreed_draw,
			partition=EXCLUDED.partition
		WHERE game.sessions.revision < EXCLUDED.revision`,
		game.ID, game.Game, game.Creator, game.Opponent, request, state, game.UpdatedAt, game.Revision, clock,
		game.TimedOut, game.Rated, game.Resigned, game.DrawOffer, game.AgreedDraw, item.partition)
	return nil
}
