package session

import (
	"context"
	"time"

	"github.com/google/uuid"

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
