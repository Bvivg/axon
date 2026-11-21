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
	ErrNoUndo      = errors.New("games with hidden cards cannot be undone")
	ErrNoDrawOffer = errors.New("there is no draw offer to answer")

	ErrNotReplayable = errors.New("only recorded moves can be replayed")
	ErrHistoryGap    = errors.New("game history has a gap")
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
	Tournament *uuid.UUID
}

func (s Session) HasPlayer(user uuid.UUID) bool {
	return s.Creator == user || s.Opponent != nil && *s.Opponent == user
}

func (s Session) Watchable(user uuid.UUID) bool {
	return s.Opponent != nil || s.HasPlayer(user)
}

type Store struct {
	pool    *pgxpool.Pool
	replica *pgxpool.Pool
	listen  *pgx.ConnConfig
	games   *engine.Registry
}

type Option func(*Store)

func ReadReplica(pool *pgxpool.Pool) Option {
	return func(s *Store) { s.replica = pool }
}

func ListenVia(config *pgx.ConnConfig) Option {
	return func(s *Store) { s.listen = config }
}

func NewStore(pool *pgxpool.Pool, games *engine.Registry, options ...Option) *Store {
	store := &Store{pool: pool, games: games}
	for _, option := range options {
		option(store)
	}
	return store
}

func (s *Store) Replica() *Store {
	if s.replica == nil {
		return s
	}
	return &Store{pool: s.replica, games: s.games}
}

const Columns = "id, game, creator_id, opponent_id, state, updated_at, revision, clock, timed_out, rated, resigned, draw_offered_by, agreed_draw, tournament_id"

func Scan(row pgx.Row) (Session, error) {
	var s Session
	var raw, clock []byte
	err := row.Scan(&s.ID, &s.Game, &s.Creator, &s.Opponent, &raw, &s.UpdatedAt, &s.Revision, &clock, &s.TimedOut, &s.Rated, &s.Resigned, &s.DrawOffer, &s.AgreedDraw, &s.Tournament)
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

func (s *Store) List(ctx context.Context, user uuid.UUID) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+Columns+` FROM game.sessions
		WHERE creator_id=$1 OR opponent_id=$1 ORDER BY updated_at DESC, id DESC LIMIT 30`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Session
	for rows.Next() {
		item, err := Scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) Live(ctx context.Context, game engine.Key, limit int) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+Columns+` FROM game.sessions
		WHERE opponent_id IS NOT NULL AND state->>'status' = 'in_progress' AND updated_at > now() - interval '6 hours'
			AND ($1 = '' OR game = $1)
		ORDER BY updated_at DESC, id DESC LIMIT $2`, string(game), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Session
	for rows.Next() {
		item, err := Scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) History(ctx context.Context, id uuid.UUID) (Session, []MoveRecord, error) {
	var game Session
	var moves []MoveRecord
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		game, err = Scan(tx.QueryRow(ctx, `SELECT `+Columns+` FROM game.sessions WHERE id=$1`, id))
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (ply) ply, player_id, data, played_at FROM game.moves WHERE session_id=$1 AND ply<=$2 ORDER BY ply, played_at DESC`, id, game.State.Ply)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var move MoveRecord
			var data []byte
			if err := rows.Scan(&move.Ply, &move.Player, &data, &move.PlayedAt); err != nil {
				return err
			}
			move.Data = data
			moves = append(moves, move)
		}
		return rows.Err()
	})
	if err != nil {
		return Session{}, nil, fmt.Errorf("session: history: %w", err)
	}
	return game, moves, nil
}

func (s *Store) TimeLimitMS(game engine.Key) int64 {
	return s.games.TimeLimit(game).Milliseconds()
}

func (s *Store) NewClock(game engine.Key, now time.Time) *Clock {
	return NewClock(now, s.TimeLimitMS(game))
}

func (s *Store) Watch(ctx context.Context, changed, matched func(uuid.UUID), ready func()) error {
	config := s.listen
	if config == nil {
		config = s.pool.Config().ConnConfig
	}
	conn, err := pgx.ConnectConfig(ctx, config)
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

func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func (s *Store) Games() *engine.Registry { return s.games }

const (
	ChangedChannel = channel
	MatchChannel   = matchChannel
)
