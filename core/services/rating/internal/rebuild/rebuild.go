package rebuild

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/rating/internal/glicko"
	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	"github.com/bvivg/axon/core/services/rating/internal/store"
)

const tolerance = 1e-6

type Source interface {
	Read(ctx context.Context, visit func(kind string, value []byte) error) error
}

type Live interface {
	Standings(ctx context.Context) ([]store.Standing, error)
	Replace(ctx context.Context, book *ledger.Book) error
}

type Board interface {
	Games(ctx context.Context) ([]string, error)
	Scores(ctx context.Context, game string) (map[uuid.UUID]float64, error)
	Replace(ctx context.Context, game string, players []ledger.Player) error
}

type Stats struct {
	Events     int
	Applied    int
	Duplicates int
	Skipped    int
	Invalid    int
}

func Recompute(ctx context.Context, source Source, system glicko.System) (*ledger.Book, Stats, error) {
	book := ledger.NewBook(system)
	var stats Stats
	err := source.Read(ctx, func(kind string, value []byte) error {
		stats.Events++
		game, err := ledger.Decode(kind, value)
		switch {
		case errors.Is(err, ledger.ErrUnrated), errors.Is(err, ledger.ErrForeign):
			stats.Skipped++
		case err != nil:
			stats.Invalid++
		case book.Apply(game):
			stats.Applied++
		default:
			stats.Duplicates++
		}
		return nil
	})
	if err != nil {
		return nil, stats, fmt.Errorf("rebuild: read %s: %w", ledger.FinishedTopic, err)
	}
	return book, stats, nil
}

type Difference struct {
	Store   string
	Game    string
	User    uuid.UUID
	Live    string
	Rebuilt string
}

func (d Difference) String() string {
	return fmt.Sprintf("%s %s %s: live %s, rebuilt %s", d.Store, d.Game, d.User, d.Live, d.Rebuilt)
}

const missing = "missing"

func describe(p ledger.Player) string {
	return fmt.Sprintf("%.6f/%.6f/%.8f %d games (%d-%d-%d)", p.Rating.Rating, p.Rating.Deviation, p.Rating.Volatility, p.Games, p.Wins, p.Losses, p.Draws)
}

func near(a, b float64) bool { return math.Abs(a-b) <= tolerance }

func same(a, b ledger.Player) bool {
	return near(a.Rating.Rating, b.Rating.Rating) && near(a.Rating.Deviation, b.Rating.Deviation) && near(a.Rating.Volatility, b.Rating.Volatility) &&
		a.Games == b.Games && a.Wins == b.Wins && a.Losses == b.Losses && a.Draws == b.Draws
}

func Compare(ctx context.Context, book *ledger.Book, live Live, board Board) ([]Difference, error) {
	standings, err := live.Standings(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebuild: read live ratings: %w", err)
	}
	var out []Difference
	seen := map[ledger.Key]bool{}
	for _, standing := range standings {
		key := ledger.Key{Game: standing.Game, User: standing.Player.User}
		seen[key] = true
		rebuilt, ok := book.Players[key]
		switch {
		case !ok:
			out = append(out, Difference{Store: "postgres", Game: key.Game, User: key.User, Live: describe(standing.Player), Rebuilt: missing})
		case !same(standing.Player, rebuilt):
			out = append(out, Difference{Store: "postgres", Game: key.Game, User: key.User, Live: describe(standing.Player), Rebuilt: describe(rebuilt)})
		}
	}
	for key, rebuilt := range book.Players {
		if !seen[key] {
			out = append(out, Difference{Store: "postgres", Game: key.Game, User: key.User, Live: missing, Rebuilt: describe(rebuilt)})
		}
	}
	games, err := boards(ctx, book, board)
	if err != nil {
		return nil, err
	}
	for _, game := range games {
		scores, err := board.Scores(ctx, game)
		if err != nil {
			return nil, fmt.Errorf("rebuild: read leaderboard %s: %w", game, err)
		}
		for user, score := range scores {
			rebuilt, ok := book.Players[ledger.Key{Game: game, User: user}]
			switch {
			case !ok:
				out = append(out, Difference{Store: "redis", Game: game, User: user, Live: fmt.Sprintf("%.6f", score), Rebuilt: missing})
			case !near(score, rebuilt.Rating.Rating):
				out = append(out, Difference{Store: "redis", Game: game, User: user, Live: fmt.Sprintf("%.6f", score), Rebuilt: fmt.Sprintf("%.6f", rebuilt.Rating.Rating)})
			}
		}
		for _, rebuilt := range book.Standings(game) {
			if _, ok := scores[rebuilt.User]; !ok {
				out = append(out, Difference{Store: "redis", Game: game, User: rebuilt.User, Live: missing, Rebuilt: fmt.Sprintf("%.6f", rebuilt.Rating.Rating)})
			}
		}
	}
	slices.SortFunc(out, func(a, b Difference) int {
		return cmp.Or(strings.Compare(a.Store, b.Store), strings.Compare(a.Game, b.Game), strings.Compare(a.User.String(), b.User.String()))
	})
	return out, nil
}

func boards(ctx context.Context, book *ledger.Book, board Board) ([]string, error) {
	existing, err := board.Games(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebuild: list leaderboards: %w", err)
	}
	games := append(existing, book.Games()...)
	slices.Sort(games)
	return slices.Compact(games), nil
}

func Apply(ctx context.Context, book *ledger.Book, live Live, board Board) error {
	if err := live.Replace(ctx, book); err != nil {
		return fmt.Errorf("rebuild: replace live ratings: %w", err)
	}
	games, err := boards(ctx, book, board)
	if err != nil {
		return err
	}
	for _, game := range games {
		if err := board.Replace(ctx, game, book.Standings(game)); err != nil {
			return fmt.Errorf("rebuild: replace leaderboard %s: %w", game, err)
		}
	}
	return nil
}

func Sync(ctx context.Context, live Live, board Board) error {
	standings, err := live.Standings(ctx)
	if err != nil {
		return fmt.Errorf("rebuild: read live ratings: %w", err)
	}
	byGame := map[string][]ledger.Player{}
	for _, standing := range standings {
		byGame[standing.Game] = append(byGame[standing.Game], standing.Player)
	}
	existing, err := board.Games(ctx)
	if err != nil {
		return fmt.Errorf("rebuild: list leaderboards: %w", err)
	}
	for _, game := range existing {
		if _, ok := byGame[game]; !ok {
			byGame[game] = nil
		}
	}
	for game, players := range byGame {
		if err := board.Replace(ctx, game, players); err != nil {
			return fmt.Errorf("rebuild: sync leaderboard %s: %w", game, err)
		}
	}
	return nil
}
