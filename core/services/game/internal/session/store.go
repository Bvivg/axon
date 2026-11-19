package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

var (
	ErrNotFound    = errors.New("game session not found")
	ErrNotPlaying  = errors.New("game is not in progress")
	ErrFull        = errors.New("game session is full")
	ErrRated       = errors.New("rated games cannot be undone")
	ErrNoDrawOffer = errors.New("there is no draw offer to answer")
)

const (
	channel      = "game_session_changed"
	matchChannel = "game_match_changed"
)

type Session struct {
	ID         uuid.UUID
	Game       engine.Key
	Creator    uuid.UUID
	Opponent   *uuid.UUID
	State      engine.State
	UpdatedAt  time.Time
	Revision   uint32
	Clock      *Clock
	TimedOut   bool
	Rated      bool
	Resigned   bool
	DrawOffer  *uuid.UUID
	AgreedDraw bool
}

func (s Session) HasPlayer(user uuid.UUID) bool {
	return s.Creator == user || s.Opponent != nil && *s.Opponent == user
}

type Store struct {
	pool  *pgxpool.Pool
	games *engine.Registry
}

func NewStore(pool *pgxpool.Pool, games *engine.Registry) *Store {
	return &Store{pool: pool, games: games}
}

const columns = "id, game, creator_id, opponent_id, state, updated_at, revision, clock, timed_out, rated, resigned, draw_offered_by, agreed_draw"

func scan(row pgx.Row) (Session, error) {
	var s Session
	var raw, clock []byte
	err := row.Scan(&s.ID, &s.Game, &s.Creator, &s.Opponent, &raw, &s.UpdatedAt, &s.Revision, &clock, &s.TimedOut, &s.Rated, &s.Resigned, &s.DrawOffer, &s.AgreedDraw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err == nil && raw != nil {
		err = json.Unmarshal(raw, &s.State)
	}
	if err == nil && clock != nil {
		err = json.Unmarshal(clock, &s.Clock)
	}
	return s, err
}

func (s *Store) Create(ctx context.Context, user, request uuid.UUID, key engine.Key) (Session, error) {
	if _, err := s.games.Get(key); err != nil {
		return Session{}, err
	}
	return scan(s.pool.QueryRow(ctx, `INSERT INTO game.sessions (id, game, creator_id, request_id)
		VALUES ($1,$2,$3,$4) ON CONFLICT (creator_id, request_id) DO UPDATE SET request_id=EXCLUDED.request_id
		RETURNING `+columns, uuid.New(), key, user, request))
}

func (s *Store) Get(ctx context.Context, id, user uuid.UUID) (Session, error) {
	return s.change(ctx, id, func(tx pgx.Tx, game Session) (Session, error) {
		if !game.HasPlayer(user) {
			return Session{}, ErrNotFound
		}
		if game.expire(time.Now()) {
			if err := s.save(ctx, tx, &game); err != nil {
				return Session{}, err
			}
		}
		return game, nil
	})
}

func (s *Store) List(ctx context.Context, user uuid.UUID) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+columns+` FROM game.sessions
		WHERE creator_id=$1 OR opponent_id=$1 ORDER BY updated_at DESC, id DESC LIMIT 30`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Session
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) Join(ctx context.Context, id, user uuid.UUID) (Session, error) {
	return s.change(ctx, id, func(tx pgx.Tx, game Session) (Session, error) {
		if game.HasPlayer(user) {
			return game, nil
		}
		if game.Opponent != nil {
			return Session{}, ErrFull
		}
		players := []engine.PlayerID{engine.PlayerID(game.Creator.String()), engine.PlayerID(user.String())}
		state, err := s.games.NewState(game.Game, players)
		if err != nil {
			return Session{}, err
		}
		game.Opponent, game.State = &user, state
		game.Clock = s.NewClock(game.Game, time.Now())
		err = s.save(ctx, tx, &game)
		return game, err
	})
}

func (s *Store) TimeLimitMS(game engine.Key) int64 {
	return s.games.TimeLimit(game).Milliseconds()
}

func (s *Store) NewClock(game engine.Key, now time.Time) *Clock {
	return NewClock(now, s.TimeLimitMS(game))
}

func (s *Store) Move(ctx context.Context, id, user uuid.UUID, ply int, kind engine.Key, data json.RawMessage, revision *uint32) (Session, error) {
	return s.change(ctx, id, func(tx pgx.Tx, game Session) (Session, error) {
		if !game.HasPlayer(user) {
			return Session{}, ErrNotFound
		}
		if game.Game != kind {
			return Session{}, engine.ErrGameMismatch
		}
		if game.Opponent == nil {
			return Session{}, ErrNotPlaying
		}
		if ply != game.State.Ply || revision != nil && *revision != game.Revision {
			return Session{}, engine.ErrStaleMove
		}
		rules, err := s.games.Get(game.Game)
		if err != nil {
			return Session{}, err
		}
		now := time.Now()
		if game.expire(now) {
			return s.persist(ctx, tx, game)
		}
		beforeState, beforeClock := game.State, game.ClockAt(now)
		move := engine.Move{Player: engine.PlayerID(user.String()), Ply: ply, Data: data}
		game.State, err = rules.ApplyMove(game.State, move)
		if err != nil {
			return Session{}, err
		}
		stateRaw, err := json.Marshal(beforeState)
		if err != nil {
			return Session{}, err
		}
		clockRaw, err := json.Marshal(beforeClock)
		if err != nil {
			return Session{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO game.moves (session_id,ply,player_id,data,state_before,clock_before) VALUES ($1,$2,$3,$4,$5,$6)`,
			game.ID, game.State.Ply, user, move.Data, stateRaw, clockRaw); err != nil {
			return Session{}, err
		}
		game.Clock, game.DrawOffer = beforeClock, nil
		err = s.save(ctx, tx, &game)
		return game, err
	})
}

func (s *Store) change(ctx context.Context, id uuid.UUID, mutate func(pgx.Tx, Session) (Session, error)) (Session, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	game, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM game.sessions WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return Session{}, err
	}
	game, err = mutate(tx, game)
	if err != nil {
		return Session{}, err
	}
	return game, tx.Commit(ctx)
}

func (s *Store) save(ctx context.Context, tx pgx.Tx, game *Session) error {
	raw, err := json.Marshal(game.State)
	if err != nil {
		return err
	}
	clock, err := json.Marshal(game.Clock)
	if err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `UPDATE game.sessions SET opponent_id=$2,state=$3,clock=$4,timed_out=$5,resigned=$6,draw_offered_by=$7,agreed_draw=$8,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1 RETURNING updated_at,revision`, game.ID, game.Opponent, raw, clock, game.TimedOut, game.Resigned, game.DrawOffer, game.AgreedDraw).Scan(&game.UpdatedAt, &game.Revision); err != nil {
		return err
	}
	if game.Rated && game.State.Over() {
		if err := s.settle(ctx, tx, *game); err != nil {
			return fmt.Errorf("session: settle rating: %w", err)
		}
	}
	_, err = tx.Exec(ctx, `SELECT pg_notify($1,$2)`, channel, game.ID.String())
	return err
}

func (s *Store) Watch(ctx context.Context, changed, matched func(uuid.UUID), ready func()) error {
	conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	if _, err := conn.Exec(ctx, "LISTEN "+channel+"; LISTEN "+matchChannel); err != nil {
		return fmt.Errorf("listen for game sessions: %w", err)
	}
	ready()
	for {
		event, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		id, err := uuid.Parse(event.Payload)
		if err != nil {
			continue
		}
		if event.Channel == matchChannel {
			matched(id)
		} else {
			changed(id)
		}
	}
}

func (s *Store) persist(ctx context.Context, tx pgx.Tx, game Session) (Session, error) {
	err := s.save(ctx, tx, &game)
	return game, err
}

func (s *Store) Leave(ctx context.Context, id, user uuid.UUID) (Session, bool, error) {
	cancelled := false
	game, err := s.change(ctx, id, func(tx pgx.Tx, game Session) (Session, error) {
		if !game.HasPlayer(user) {
			return Session{}, ErrNotFound
		}
		if game.Opponent == nil {
			if _, err := tx.Exec(ctx, `DELETE FROM game.sessions WHERE id=$1 AND opponent_id IS NULL`, game.ID); err != nil {
				return Session{}, err
			}
			cancelled = true
			_, err := tx.Exec(ctx, `SELECT pg_notify($1,$2)`, channel, game.ID.String())
			return game, err
		}
		now := time.Now()
		if game.expire(now) || game.State.Over() {
			return s.persist(ctx, tx, game)
		}
		seat, ok := game.State.Seat(engine.PlayerID(user.String()))
		if !ok {
			return Session{}, ErrNotFound
		}
		game.Clock = game.ClockAt(now)
		game.State = game.State.Finish(1 - seat)
		game.Resigned, game.DrawOffer = true, nil
		return s.persist(ctx, tx, game)
	})
	return game, cancelled, err
}
