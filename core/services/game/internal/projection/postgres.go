package projection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

const OwnerChannel = "game_owner_changed"

type Postgres struct {
	pool  *pgxpool.Pool
	store *session.Store
}

func New(store *session.Store) *Postgres {
	return &Postgres{pool: store.Pool(), store: store}
}

func (m *Postgres) Claim(ctx context.Context, partition int32, ownerID, address string) (int64, error) {
	var generation int64
	err := pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO game.owners (partition, owner_id, address, generation) VALUES ($1,$2,$3,1)
			ON CONFLICT (partition) DO UPDATE SET owner_id=EXCLUDED.owner_id, address=EXCLUDED.address,
				generation=game.owners.generation+1, updated_at=clock_timestamp()
			RETURNING generation`, partition, ownerID, address).Scan(&generation); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `SELECT pg_notify($1,$2)`, OwnerChannel, fmt.Sprint(partition))
		return err
	})
	return generation, err
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
	rows, err := m.pool.Query(ctx, `SELECT partition, address FROM game.owners`)
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

func (m *Postgres) Apply(ctx context.Context, partition int32, entries []journal.Entry) ([]uuid.UUID, error) {
	var changed []uuid.UUID
	err := pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		changed = changed[:0]
		var next, generation int64
		err := tx.QueryRow(ctx, `SELECT next_offset, claim_generation FROM game.projector_offsets WHERE partition=$1 FOR UPDATE`, partition).Scan(&next, &generation)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := tx.Exec(ctx, `INSERT INTO game.projector_offsets (partition, next_offset) VALUES ($1,0)`, partition); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		fence := journal.NewFence()
		fence.Seed(partition, generation)
		seen := map[uuid.UUID]bool{}
		for _, entry := range entries {
			if entry.Offset < next {
				continue
			}
			next = entry.Offset + 1
			if !fence.Admit(entry) {
				continue
			}
			if err := m.apply(ctx, tx, partition, *entry.Event); err != nil {
				return fmt.Errorf("projection: apply offset %d: %w", entry.Offset, err)
			}
			id := entry.Event.Session.ID
			if !seen[id] {
				seen[id] = true
				changed = append(changed, id)
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE game.projector_offsets SET next_offset=$2, claim_generation=$3, updated_at=clock_timestamp() WHERE partition=$1`,
			partition, next, fence.Generation(partition)); err != nil {
			return err
		}
		for _, id := range changed {
			if _, err := tx.Exec(ctx, `SELECT pg_notify($1,$2)`, session.ChangedChannel, id.String()); err != nil {
				return err
			}
		}
		return nil
	})
	return changed, err
}

func (m *Postgres) apply(ctx context.Context, tx pgx.Tx, partition int32, event journal.Event) error {
	game, change := event.Session, event.Change
	if change.Fact == session.FactCancelled {
		if _, err := tx.Exec(ctx, `DELETE FROM game.moves WHERE session_id=$1`, game.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM game.sessions WHERE id=$1 AND NOT EXISTS (SELECT 1 FROM game.rating_changes WHERE session_id=$1)`, game.ID)
		return err
	}
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
	if change.Request != uuid.Nil {
		request = change.Request
	}
	if _, err := tx.Exec(ctx, `INSERT INTO game.sessions (id, game, creator_id, opponent_id, request_id, state, updated_at, revision, clock,
			timed_out, rated, resigned, draw_offered_by, agreed_draw, partition)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (id) DO UPDATE SET opponent_id=EXCLUDED.opponent_id, state=EXCLUDED.state, updated_at=EXCLUDED.updated_at,
			revision=EXCLUDED.revision, clock=EXCLUDED.clock, timed_out=EXCLUDED.timed_out, rated=EXCLUDED.rated,
			resigned=EXCLUDED.resigned, draw_offered_by=EXCLUDED.draw_offered_by, agreed_draw=EXCLUDED.agreed_draw,
			partition=EXCLUDED.partition
		WHERE game.sessions.revision < EXCLUDED.revision`,
		game.ID, game.Game, game.Creator, game.Opponent, request, state, game.UpdatedAt, game.Revision, clock,
		game.TimedOut, game.Rated, game.Resigned, game.DrawOffer, game.AgreedDraw, partition); err != nil {
		return err
	}
	switch change.Fact {
	case session.FactMoved:
		before, err := json.Marshal(change.Move.StateBefore)
		if err != nil {
			return err
		}
		clockBefore, err := json.Marshal(change.Move.ClockBefore)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO game.moves (session_id, ply, player_id, data, state_before, clock_before) VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (session_id, ply) DO UPDATE SET player_id=EXCLUDED.player_id, data=EXCLUDED.data,
				state_before=EXCLUDED.state_before, clock_before=EXCLUDED.clock_before`,
			game.ID, change.Move.Ply, change.Move.Player, []byte(change.Move.Data), before, clockBefore); err != nil {
			return err
		}
	case session.FactUndone:
		if _, err := tx.Exec(ctx, `DELETE FROM game.moves WHERE session_id=$1 AND ply=$2`, game.ID, change.UndonePly); err != nil {
			return err
		}
	}
	if game.Rated && game.Opponent != nil && game.State.Status != engine.StatusInProgress {
		if err := m.store.Settle(ctx, tx, game); err != nil {
			return fmt.Errorf("settle rating: %w", err)
		}
	}
	return nil
}
