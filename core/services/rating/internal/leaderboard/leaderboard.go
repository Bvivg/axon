package leaderboard

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/services/rating/internal/ledger"
)

const prefix = "leaderboard:"

func Key(game string) string { return prefix + game }

type Entry struct {
	User  uuid.UUID
	Score float64
}

type Redis struct {
	client redis.Cmdable
}

func New(client redis.Cmdable) *Redis { return &Redis{client: client} }

func members(players []ledger.Player) []redis.Z {
	out := make([]redis.Z, len(players))
	for i, player := range players {
		out[i] = redis.Z{Score: player.Rating.Rating, Member: player.User.String()}
	}
	return out
}

func (r *Redis) Update(ctx context.Context, game string, players ...ledger.Player) error {
	if len(players) == 0 {
		return nil
	}
	if err := r.client.ZAdd(ctx, Key(game), members(players)...).Err(); err != nil {
		return fmt.Errorf("leaderboard: update %s: %w", game, err)
	}
	return nil
}

func (r *Redis) Replace(ctx context.Context, game string, players []ledger.Player) error {
	if len(players) == 0 {
		return r.client.Del(ctx, Key(game)).Err()
	}
	staging := Key(game) + ":staging"
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, staging)
	for start := 0; start < len(players); start += 1000 {
		pipe.ZAdd(ctx, staging, members(players[start:min(start+1000, len(players))])...)
	}
	pipe.Rename(ctx, staging, Key(game))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("leaderboard: replace %s: %w", game, err)
	}
	return nil
}

func (r *Redis) Scores(ctx context.Context, game string) (map[uuid.UUID]float64, error) {
	entries, err := r.client.ZRangeWithScores(ctx, Key(game), 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("leaderboard: read %s: %w", game, err)
	}
	out := make(map[uuid.UUID]float64, len(entries))
	for _, entry := range entries {
		member, _ := entry.Member.(string)
		user, err := uuid.Parse(member)
		if err != nil {
			return nil, fmt.Errorf("leaderboard: %s holds a foreign member %q", game, member)
		}
		out[user] = entry.Score
	}
	return out, nil
}

func (r *Redis) Games(ctx context.Context) ([]string, error) {
	var games []string
	iter := r.client.Scan(ctx, 0, prefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		game := strings.TrimPrefix(iter.Val(), prefix)
		if ledger.ValidGame(game) {
			games = append(games, game)
		}
	}
	return games, iter.Err()
}

func (r *Redis) Top(ctx context.Context, game string, offset, limit int) ([]Entry, int64, error) {
	key := Key(game)
	pipe := r.client.Pipeline()
	total := pipe.ZCard(ctx, key)
	page := pipe.ZRevRangeWithScores(ctx, key, int64(offset), int64(offset+limit-1))
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, 0, fmt.Errorf("leaderboard: top %s: %w", game, err)
	}
	out := make([]Entry, 0, len(page.Val()))
	for _, entry := range page.Val() {
		member, _ := entry.Member.(string)
		user, err := uuid.Parse(member)
		if err != nil {
			return nil, 0, fmt.Errorf("leaderboard: %s holds a foreign member %q", game, member)
		}
		out = append(out, Entry{User: user, Score: entry.Score})
	}
	return out, total.Val(), nil
}
