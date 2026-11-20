package session

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/rating"
)

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

func (s *Store) Settle(ctx context.Context, tx pgx.Tx, game Session) error {
	if game.Opponent == nil {
		return nil
	}
	var settled bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM game.rating_changes WHERE session_id=$1)`, game.ID).Scan(&settled); err != nil {
		return err
	}
	if settled {
		return nil
	}
	players := []uuid.UUID{game.Creator, *game.Opponent}
	locked := slices.Clone(players)
	slices.SortFunc(locked, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	if _, err := tx.Exec(ctx, `INSERT INTO game.ratings (user_id, game) VALUES ($1,$3),($2,$3) ON CONFLICT DO NOTHING`, locked[0], locked[1], game.Game); err != nil {
		return err
	}
	current := map[uuid.UUID]rating.Player{}
	rows, err := tx.Query(ctx, `SELECT user_id, rating, games FROM game.ratings WHERE game=$1 AND user_id = ANY($2) ORDER BY user_id FOR UPDATE`, game.Game, locked)
	if err != nil {
		return err
	}
	for rows.Next() {
		var user uuid.UUID
		var player rating.Player
		if err := rows.Scan(&user, &player.Rating, &player.Games); err != nil {
			rows.Close()
			return err
		}
		current[user] = player
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(current) != 2 {
		return fmt.Errorf("expected two ratings, got %d", len(current))
	}
	for seat, user := range players {
		opponent := players[1-seat]
		score := scoreFor(game.State, seat)
		after := rating.Next(current[user], current[opponent], score)
		win, loss, draw := 0, 0, 0
		switch score {
		case rating.Win:
			win = 1
		case rating.Loss:
			loss = 1
		default:
			draw = 1
		}
		if _, err := tx.Exec(ctx, `UPDATE game.ratings SET rating=$3, games=games+1, wins=wins+$4, losses=losses+$5, draws=draws+$6, updated_at=clock_timestamp()
			WHERE user_id=$1 AND game=$2`, user, game.Game, after, win, loss, draw); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO game.rating_changes (session_id, user_id, game, before, after) VALUES ($1,$2,$3,$4,$5)`,
			game.ID, user, game.Game, current[user].Rating, after); err != nil {
			return err
		}
	}
	return nil
}

func scoreFor(state engine.State, seat int) rating.Score {
	if state.Status == engine.StatusDraw || state.Winner < 0 {
		return rating.Draw
	}
	if state.Winner == seat {
		return rating.Win
	}
	return rating.Loss
}

func (s *Store) Ratings(ctx context.Context, game engine.Key, users []uuid.UUID) (map[uuid.UUID]Rating, error) {
	result := make(map[uuid.UUID]Rating, len(users))
	for _, user := range users {
		result[user] = Rating{User: user, Game: game, Rating: rating.Initial}
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
