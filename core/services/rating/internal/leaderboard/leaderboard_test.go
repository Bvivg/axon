package leaderboard_test

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/services/rating/internal/glicko"
	"github.com/bvivg/axon/core/services/rating/internal/leaderboard"
	"github.com/bvivg/axon/core/services/rating/internal/ledger"
)

func board(t *testing.T) (*leaderboard.Redis, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return leaderboard.New(client), server
}

func player(user uuid.UUID, rating float64) ledger.Player {
	return ledger.Player{User: user, Rating: glicko.Rating{Rating: rating, Deviation: 300, Volatility: 0.06}}
}

var (
	alice = uuid.MustParse("1d6f2f43-9b35-4a6f-8f43-2a1c9d0e7b11")
	bob   = uuid.MustParse("2e7a3b54-ac46-4b70-9054-3b2dae1f8c22")
	carol = uuid.MustParse("3f8b4c65-bd57-4c81-a165-4c3ebf209d33")
)

func TestUpdateKeepsTheLatestScore(t *testing.T) {
	ctx := context.Background()
	b, _ := board(t)
	if err := b.Update(ctx, "chess", player(alice, 1362.3145), player(bob, 1037.6855)); err != nil {
		t.Fatal(err)
	}
	if err := b.Update(ctx, "chess", player(alice, 1400.125)); err != nil {
		t.Fatal(err)
	}
	scores, err := b.Scores(ctx, "chess")
	if err != nil {
		t.Fatal(err)
	}
	if want := map[uuid.UUID]float64{alice: 1400.125, bob: 1037.6855}; !reflect.DeepEqual(scores, want) {
		t.Fatalf("Scores() = %v, want %v", scores, want)
	}
}

func TestTopPagesFromTheHighestRating(t *testing.T) {
	ctx := context.Background()
	b, _ := board(t)
	if err := b.Update(ctx, "quoridor", player(alice, 1250), player(bob, 1500.5), player(carol, 1100)); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		offset, limit int
		want          []leaderboard.Entry
	}{
		{name: "first page", offset: 0, limit: 2, want: []leaderboard.Entry{{User: bob, Score: 1500.5}, {User: alice, Score: 1250}}},
		{name: "second page", offset: 2, limit: 2, want: []leaderboard.Entry{{User: carol, Score: 1100}}},
		{name: "past the end", offset: 5, limit: 2, want: []leaderboard.Entry{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := b.Top(ctx, "quoridor", tt.offset, tt.limit)
			if err != nil || total != 3 || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Top() = %v, %d, %v; want %v", got, total, err, tt.want)
			}
		})
	}
}

func TestReplaceSwapsTheWholeBoard(t *testing.T) {
	ctx := context.Background()
	b, server := board(t)
	if err := b.Update(ctx, "chess", player(alice, 1300), player(carol, 900)); err != nil {
		t.Fatal(err)
	}
	if err := b.Update(ctx, "tictactoe", player(bob, 1210)); err != nil {
		t.Fatal(err)
	}
	if err := b.Replace(ctx, "chess", []ledger.Player{player(alice, 1290), player(bob, 1111)}); err != nil {
		t.Fatal(err)
	}
	scores, err := b.Scores(ctx, "chess")
	if err != nil || !reflect.DeepEqual(scores, map[uuid.UUID]float64{alice: 1290, bob: 1111}) {
		t.Fatalf("Scores() = %v, %v", scores, err)
	}
	if server.Exists(leaderboard.Key("chess") + ":staging") {
		t.Fatal("staging key left behind")
	}
	if err := b.Replace(ctx, "tictactoe", nil); err != nil {
		t.Fatal(err)
	}
	games, err := b.Games(ctx)
	if err != nil || !slices.Equal(games, []string{"chess"}) {
		t.Fatalf("Games() = %v, %v", games, err)
	}
}

func TestScoresRejectsForeignMembers(t *testing.T) {
	ctx := context.Background()
	b, server := board(t)
	if _, err := server.ZAdd(leaderboard.Key("chess"), 1, "not-a-user"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Scores(ctx, "chess"); err == nil {
		t.Fatal("a foreign member was accepted")
	}
}
