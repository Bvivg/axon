package session

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

func (s *Store) Undo(ctx context.Context, id, user uuid.UUID, revision *uint32) (Session, error) {
	return s.change(ctx, id, func(tx pgx.Tx, game Session) (Session, error) {
		if !game.HasPlayer(user) {
			return Session{}, ErrNotFound
		}
		if game.Rated {
			return Session{}, ErrRated
		}
		if revision != nil && *revision != game.Revision {
			return Session{}, engine.ErrStaleMove
		}
		if game.Opponent == nil || game.State.Ply == 0 {
			return Session{}, ErrNotPlaying
		}
		var stateRaw, clockRaw []byte
		if err := tx.QueryRow(ctx, `SELECT state_before,clock_before FROM game.moves WHERE session_id=$1 AND ply=$2`, id, game.State.Ply).Scan(&stateRaw, &clockRaw); err != nil {
			return Session{}, err
		}
		previous, err := s.previous(ctx, tx, game, stateRaw)
		if err != nil {
			return Session{}, err
		}
		clock := NewClock(time.Now())
		if len(clockRaw) > 0 && string(clockRaw) != "null" {
			if err := json.Unmarshal(clockRaw, clock); err != nil {
				return Session{}, err
			}
			clock.StartedAt = time.Now()
		}
		if _, err := tx.Exec(ctx, `DELETE FROM game.moves WHERE session_id=$1 AND ply=$2`, id, game.State.Ply); err != nil {
			return Session{}, err
		}
		game.State, game.Clock, game.TimedOut = previous, clock, false
		return s.persist(ctx, tx, game)
	})
}

func (s *Store) previous(ctx context.Context, tx pgx.Tx, game Session, raw []byte) (engine.State, error) {
	var state engine.State
	if len(raw) > 0 && string(raw) != "null" {
		err := json.Unmarshal(raw, &state)
		return state, err
	}
	state, err := s.games.NewState(game.Game, game.State.Players)
	if err != nil {
		return state, err
	}
	rules, err := s.games.Get(game.Game)
	if err != nil {
		return state, err
	}
	rows, err := tx.Query(ctx, `SELECT player_id,data FROM game.moves WHERE session_id=$1 AND ply<$2 ORDER BY ply`, game.ID, game.State.Ply)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var user uuid.UUID
		var data []byte
		if err := rows.Scan(&user, &data); err != nil {
			return state, err
		}
		state, err = rules.ApplyMove(state, engine.Move{Player: engine.PlayerID(user.String()), Ply: state.Ply, Data: data})
		if err != nil {
			return state, err
		}
	}
	return state, rows.Err()
}

func (s *Store) Expire(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT id FROM game.sessions
		WHERE state->>'status'='in_progress' AND clock IS NOT NULL
		AND (clock->>'started_at')::timestamptz +
		((clock->'remaining_ms'->>((state->>'turn')::int))::bigint * interval '1 millisecond') <= clock_timestamp()
		LIMIT 100`)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.change(ctx, id, func(tx pgx.Tx, game Session) (Session, error) {
			if game.expire(time.Now()) {
				return s.persist(ctx, tx, game)
			}
			return game, nil
		}); err != nil {
			return err
		}
	}
	return nil
}
