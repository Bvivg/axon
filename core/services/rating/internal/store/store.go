package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bvivg/axon/core/services/rating/internal/glicko"
	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

var ErrAhead = errors.New("store: the live consumer processed events the rebuild did not see")

type Applied struct {
	Game   ledger.Game
	Before []ledger.Player
	After  []ledger.Player
}

type Standing struct {
	Game   string
	Player ledger.Player
}

type Postgres struct {
	pool   *pgxpool.Pool
	system glicko.System
}

func New(pool *pgxpool.Pool, system glicko.System) *Postgres {
	return &Postgres{pool: pool, system: system}
}

const columns = `user_id, rating, deviation, volatility, games, wins, losses, draws`

func scan(row pgx.Row) (ledger.Player, error) {
	var p ledger.Player
	err := row.Scan(&p.User, &p.Rating.Rating, &p.Rating.Deviation, &p.Rating.Volatility, &p.Games, &p.Wins, &p.Losses, &p.Draws)
	return p, err
}

func (s *Postgres) Apply(ctx context.Context, game ledger.Game) (Applied, bool, error) {
	var out Applied
	applied := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO rating.processed_events (event_id, session_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, game.EventID, game.Session)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		var settled bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM rating.changes WHERE session_id = $1)`, game.Session).Scan(&settled); err != nil || settled {
			return err
		}
		locked := slices.Clone(game.Players)
		slices.SortFunc(locked, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
		fresh := glicko.Newcomer
		if _, err := tx.Exec(ctx, `INSERT INTO rating.ratings (game, user_id, rating, deviation, volatility)
			SELECT $1, u, $3, $4, $5 FROM unnest($2::uuid[]) AS u ON CONFLICT DO NOTHING`,
			game.Game, locked, fresh.Rating, fresh.Deviation, fresh.Volatility); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+columns+` FROM rating.ratings WHERE game = $1 AND user_id = ANY($2) ORDER BY user_id FOR UPDATE`, game.Game, locked)
		if err != nil {
			return err
		}
		current, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ledger.Player, error) { return scan(row) })
		if err != nil {
			return err
		}
		out.Before = make([]ledger.Player, len(game.Players))
		for _, player := range current {
			if seat := slices.Index(game.Players, player.User); seat >= 0 {
				out.Before[seat] = player
			}
		}
		for seat, user := range game.Players {
			if out.Before[seat].User != user {
				return fmt.Errorf("store: expected %d ratings for session %s, got %d", len(game.Players), game.Session, len(current))
			}
		}
		out.Game, out.After = game, ledger.Settle(s.system, game, out.Before)
		if err := write(ctx, tx, game, out.Before, out.After); err != nil {
			return err
		}
		if err := outbox.Write(ctx, tx, ledger.OutboxTable, ledger.Announcement(game, out.Before, out.After)); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err != nil {
		return Applied{}, false, fmt.Errorf("store: apply %s: %w", game.EventID, err)
	}
	return out, applied, nil
}

func write(ctx context.Context, tx pgx.Tx, game ledger.Game, before, after []ledger.Player) error {
	for seat, player := range after {
		if _, err := tx.Exec(ctx, `UPDATE rating.ratings SET rating=$3, deviation=$4, volatility=$5, games=$6, wins=$7, losses=$8, draws=$9, updated_at=clock_timestamp()
			WHERE game=$1 AND user_id=$2`, game.Game, player.User, player.Rating.Rating, player.Rating.Deviation, player.Rating.Volatility,
			player.Games, player.Wins, player.Losses, player.Draws); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO rating.changes (session_id, user_id, game, event_id, rating_before, rating_after,
				deviation_before, deviation_after, volatility_after, finished_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			game.Session, player.User, game.Game, game.EventID, before[seat].Rating.Rating, player.Rating.Rating,
			before[seat].Rating.Deviation, player.Rating.Deviation, player.Rating.Volatility, game.FinishedAt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Postgres) Standings(ctx context.Context) ([]Standing, error) {
	rows, err := s.pool.Query(ctx, `SELECT game, `+columns+` FROM rating.ratings ORDER BY game, user_id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Standing, error) {
		var out Standing
		p := &out.Player
		err := row.Scan(&out.Game, &p.User, &p.Rating.Rating, &p.Rating.Deviation, &p.Rating.Volatility, &p.Games, &p.Wins, &p.Losses, &p.Draws)
		return out, err
	})
}

func (s *Postgres) Players(ctx context.Context, game string, users []uuid.UUID) (map[uuid.UUID]ledger.Player, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+columns+` FROM rating.ratings WHERE game = $1 AND user_id = ANY($2)`, game, users)
	if err != nil {
		return nil, err
	}
	players, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ledger.Player, error) { return scan(row) })
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]ledger.Player, len(players))
	for _, player := range players {
		out[player.User] = player
	}
	return out, nil
}

func (s *Postgres) Top(ctx context.Context, game string, offset, limit int) ([]ledger.Player, int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM rating.ratings WHERE game = $1`, game).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+columns+` FROM rating.ratings WHERE game = $1 ORDER BY rating DESC, user_id DESC OFFSET $2 LIMIT $3`, game, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	players, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ledger.Player, error) { return scan(row) })
	return players, total, err
}

func (s *Postgres) Replace(ctx context.Context, book *ledger.Book) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE rating.processed_events, rating.changes, rating.ratings IN EXCLUSIVE MODE`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT event_id FROM rating.processed_events`)
		if err != nil {
			return err
		}
		processed, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		var unseen int
		for _, id := range processed {
			if _, ok := book.Events[id]; !ok {
				unseen++
			}
		}
		if unseen > 0 {
			return fmt.Errorf("%w: %d events", ErrAhead, unseen)
		}
		if _, err := tx.Exec(ctx, `TRUNCATE rating.changes, rating.ratings, rating.processed_events`); err != nil {
			return err
		}
		now := time.Now().UTC()
		var ratings [][]any
		for key, p := range book.Players {
			ratings = append(ratings, []any{key.Game, p.User, p.Rating.Rating, p.Rating.Deviation, p.Rating.Volatility, p.Games, p.Wins, p.Losses, p.Draws, now})
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"rating", "ratings"},
			[]string{"game", "user_id", "rating", "deviation", "volatility", "games", "wins", "losses", "draws", "updated_at"}, pgx.CopyFromRows(ratings)); err != nil {
			return err
		}
		changes := make([][]any, 0, len(book.Changes))
		for _, c := range book.Changes {
			changes = append(changes, []any{c.Session, c.After.User, c.Game, c.EventID, c.Before.Rating.Rating, c.After.Rating.Rating,
				c.Before.Rating.Deviation, c.After.Rating.Deviation, c.After.Rating.Volatility, c.At})
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"rating", "changes"},
			[]string{"session_id", "user_id", "game", "event_id", "rating_before", "rating_after", "deviation_before", "deviation_after", "volatility_after", "finished_at"},
			pgx.CopyFromRows(changes)); err != nil {
			return err
		}
		events := make([][]any, 0, len(book.Events))
		for id, session := range book.Events {
			events = append(events, []any{id, session, now})
		}
		_, err = tx.CopyFrom(ctx, pgx.Identifier{"rating", "processed_events"}, []string{"event_id", "session_id", "processed_at"}, pgx.CopyFromRows(events))
		return err
	})
}

func (s *Postgres) PruneOutbox(ctx context.Context, every, olderThan time.Duration, log *slog.Logger) {
	for {
		removed, err := outbox.Cleanup(ctx, s.pool, ledger.OutboxTable, olderThan)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			log.WarnContext(ctx, "outbox cleanup failed", "error", err)
		case removed > 0:
			log.InfoContext(ctx, "outbox rows removed", "count", removed)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
