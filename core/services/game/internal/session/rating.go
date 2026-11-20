package session

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

const InitialRating = 1200

type Rating struct {
	User   uuid.UUID
	Game   engine.Key
	Rating int
	Games  int
	Wins   int
	Losses int
	Draws  int
}

type RatingChange struct {
	Session uuid.UUID
	User    uuid.UUID
	Before  int
	After   int
	At      time.Time
}

type RatedPlayer struct {
	User   uuid.UUID
	Before int
	After  int
	Games  int
	Wins   int
	Losses int
	Draws  int
}

type RatingUpdate struct {
	Session uuid.UUID
	Game    engine.Key
	Players []RatedPlayer
	At      time.Time
}

func (s *Store) ApplyRatings(ctx context.Context, update RatingUpdate) error {
	if len(update.Players) == 0 {
		return nil
	}
	players := slices.Clone(update.Players)
	slices.SortFunc(players, func(a, b RatedPlayer) int { return slices.Compare(a.User[:], b.User[:]) })
	users := make([]uuid.UUID, len(players))
	ratings, games, wins, losses, draws, before := make([]int32, len(players)), make([]int32, len(players)), make([]int32, len(players)),
		make([]int32, len(players)), make([]int32, len(players)), make([]int32, len(players))
	for i, player := range players {
		users[i] = player.User
		ratings[i], games[i], wins[i] = int32(max(player.After, 1)), int32(player.Games), int32(player.Wins)
		losses[i], draws[i], before[i] = int32(player.Losses), int32(player.Draws), int32(max(player.Before, 1))
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO game.ratings (user_id, game, rating, games, wins, losses, draws, updated_at)
			SELECT u, $1, r, g, w, l, d, clock_timestamp()
			FROM unnest($2::uuid[], $3::int[], $4::int[], $5::int[], $6::int[], $7::int[]) AS t(u, r, g, w, l, d)
			ON CONFLICT (user_id, game) DO UPDATE SET rating=EXCLUDED.rating, games=EXCLUDED.games, wins=EXCLUDED.wins,
				losses=EXCLUDED.losses, draws=EXCLUDED.draws, updated_at=EXCLUDED.updated_at`,
			update.Game, users, ratings, games, wins, losses, draws); err != nil {
			return err
		}
		if update.Session == uuid.Nil {
			return nil
		}
		tag, err := tx.Exec(ctx, `INSERT INTO game.rating_changes (session_id, user_id, game, before, after, created_at)
			SELECT $1, u, $2, b, a, $5
			FROM unnest($3::uuid[], $4::int[], $6::int[]) AS t(u, b, a)
			WHERE EXISTS (SELECT 1 FROM game.sessions WHERE id = $1)
			ON CONFLICT (session_id, user_id) DO NOTHING`,
			update.Session, update.Game, users, before, update.At, ratings)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		_, err = tx.Exec(ctx, `SELECT pg_notify($1, $2)`, ChangedChannel, update.Session.String())
		return err
	})
}

func (s *Store) Ratings(ctx context.Context, game engine.Key, users []uuid.UUID) (map[uuid.UUID]Rating, error) {
	result := make(map[uuid.UUID]Rating, len(users))
	for _, user := range users {
		result[user] = Rating{User: user, Game: game, Rating: InitialRating}
	}
	rows, err := s.pool.Query(ctx, `SELECT user_id, rating, games, wins, losses, draws FROM game.ratings WHERE game=$1 AND user_id = ANY($2)`, game, users)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item := Rating{Game: game}
		if err := rows.Scan(&item.User, &item.Rating, &item.Games, &item.Wins, &item.Losses, &item.Draws); err != nil {
			return nil, err
		}
		result[item.User] = item
	}
	return result, rows.Err()
}

func (s *Store) Changes(ctx context.Context, sessions []uuid.UUID) (map[uuid.UUID][]RatingChange, error) {
	result := map[uuid.UUID][]RatingChange{}
	if len(sessions) == 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT session_id, user_id, before, after, created_at FROM game.rating_changes WHERE session_id = ANY($1) ORDER BY session_id, user_id`, sessions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item RatingChange
		if err := rows.Scan(&item.Session, &item.User, &item.Before, &item.After, &item.At); err != nil {
			return nil, err
		}
		result[item.Session] = append(result[item.Session], item)
	}
	return result, rows.Err()
}
