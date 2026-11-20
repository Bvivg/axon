package pairing_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/tournament/internal/pairing"
)

func users(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1))
	}
	return out
}

func entrants(ids []uuid.UUID) []pairing.Entrant {
	out := make([]pairing.Entrant, len(ids))
	for i, id := range ids {
		out[i] = pairing.Entrant{User: id, Seed: i + 1}
	}
	return out
}

func TestBracketSizeAndRounds(t *testing.T) {
	for _, tt := range []struct{ players, size, rounds int }{
		{2, 2, 1}, {3, 4, 2}, {4, 4, 2}, {5, 8, 3}, {8, 8, 3}, {9, 16, 4}, {16, 16, 4},
	} {
		if got := pairing.BracketSize(tt.players); got != tt.size {
			t.Errorf("BracketSize(%d) = %d, want %d", tt.players, got, tt.size)
		}
		if got := pairing.Rounds(tt.players); got != tt.rounds {
			t.Errorf("Rounds(%d) = %d, want %d", tt.players, got, tt.rounds)
		}
	}
}

func TestSeedOrderKeepsTopSeedsApart(t *testing.T) {
	for _, tt := range []struct {
		size int
		want []int
	}{
		{2, []int{1, 2}},
		{4, []int{1, 4, 2, 3}},
		{8, []int{1, 8, 4, 5, 2, 7, 3, 6}},
		{16, []int{1, 16, 8, 9, 4, 13, 5, 12, 2, 15, 7, 10, 3, 14, 6, 11}},
	} {
		if got := pairing.SeedOrder(tt.size); !slices.Equal(got, tt.want) {
			t.Errorf("SeedOrder(%d) = %v, want %v", tt.size, got, tt.want)
		}
	}
}

func TestBracketGivesByesToTheTopSeeds(t *testing.T) {
	ids := users(16)
	type board struct{ first, second int }
	for _, tt := range []struct {
		players int
		want    []board
	}{
		{players: 4, want: []board{{1, 4}, {2, 3}}},
		{players: 8, want: []board{{1, 8}, {4, 5}, {2, 7}, {3, 6}}},
		{players: 5, want: []board{{1, 0}, {4, 5}, {2, 0}, {3, 0}}},
		{players: 6, want: []board{{1, 0}, {4, 5}, {2, 0}, {3, 6}}},
		{players: 3, want: []board{{1, 0}, {2, 3}}},
		{players: 12, want: []board{{1, 0}, {8, 9}, {4, 0}, {5, 12}, {2, 0}, {7, 10}, {3, 0}, {6, 11}}},
	} {
		t.Run(fmt.Sprint(tt.players), func(t *testing.T) {
			pairs, err := pairing.Bracket(entrants(ids[:tt.players]))
			if err != nil {
				t.Fatal(err)
			}
			if len(pairs) != len(tt.want) {
				t.Fatalf("%d boards, want %d", len(pairs), len(tt.want))
			}
			byes := 0
			for i, pair := range pairs {
				want := [2]uuid.UUID{ids[tt.want[i].first-1], uuid.Nil}
				if tt.want[i].second > 0 {
					want[1] = ids[tt.want[i].second-1]
				} else {
					byes++
				}
				if pair.Board != i+1 || pair.Seats != want || pair.Bye() != (tt.want[i].second == 0) {
					t.Fatalf("board %d = %+v, want %v", i+1, pair, want)
				}
			}
			if byes != pairing.BracketSize(tt.players)-tt.players {
				t.Fatalf("%d byes", byes)
			}
		})
	}
}

func TestBracketRejectsBadSeeds(t *testing.T) {
	ids := users(4)
	for name, in := range map[string][]pairing.Entrant{
		"one player":     entrants(ids[:1]),
		"duplicate seed": {{User: ids[0], Seed: 1}, {User: ids[1], Seed: 1}},
		"seed too large": {{User: ids[0], Seed: 1}, {User: ids[1], Seed: 3}},
	} {
		if _, err := pairing.Bracket(in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := pairing.Bracket(nil); !errors.Is(err, pairing.ErrPlayers) {
		t.Errorf("empty bracket = %v", err)
	}
}

func TestAdvancePutsTheBetterSeedFirst(t *testing.T) {
	ids := users(8)
	pairs, err := pairing.Advance([]pairing.Entrant{{ids[0], 1}, {ids[4], 5}, {ids[6], 7}, {ids[2], 3}})
	if err != nil {
		t.Fatal(err)
	}
	want := []pairing.Pair{{Board: 1, Seats: [2]uuid.UUID{ids[0], ids[4]}}, {Board: 2, Seats: [2]uuid.UUID{ids[2], ids[6]}}}
	if !slices.Equal(pairs, want) {
		t.Fatalf("pairs = %+v", pairs)
	}
	if _, err := pairing.Advance([]pairing.Entrant{{ids[0], 1}, {ids[1], 2}, {ids[2], 3}}); !errors.Is(err, pairing.ErrPlayers) {
		t.Fatalf("odd winners = %v", err)
	}
}

func contenders(ids []uuid.UUID) []pairing.Contender {
	out := make([]pairing.Contender, len(ids))
	for i, id := range ids {
		out[i] = pairing.Contender{User: id, Seed: i + 1, Rating: 1500}
	}
	return out
}

func seats(pairs []pairing.Pair) map[[2]uuid.UUID]bool {
	out := map[[2]uuid.UUID]bool{}
	for _, p := range pairs {
		out[p.Seats] = true
	}
	return out
}

func TestSwissPairsByScoreThenRating(t *testing.T) {
	ids := users(6)
	players := contenders(ids)
	players[0].HalfPoints, players[1].HalfPoints, players[2].HalfPoints = 2, 2, 2
	players[3].HalfPoints, players[4].HalfPoints, players[5].HalfPoints = 0, 0, 0
	players[2].Rating = 1700
	players[0].Opponents, players[3].Opponents = []uuid.UUID{ids[3]}, []uuid.UUID{ids[0]}
	players[1].Opponents, players[4].Opponents = []uuid.UUID{ids[4]}, []uuid.UUID{ids[1]}
	players[2].Opponents, players[5].Opponents = []uuid.UUID{ids[5]}, []uuid.UUID{ids[2]}
	ranked := pairing.Rank(players)
	if ranked[0].User != ids[2] || ranked[1].User != ids[0] || ranked[2].User != ids[1] {
		t.Fatalf("rank = %v %v %v", ranked[0].Seed, ranked[1].Seed, ranked[2].Seed)
	}
	pairs, err := pairing.Swiss(players)
	if err != nil {
		t.Fatal(err)
	}
	got := seats(pairs)
	for _, want := range [][2]uuid.UUID{{ids[2], ids[0]}, {ids[1], ids[3]}, {ids[4], ids[5]}} {
		if !got[want] {
			t.Fatalf("pairs = %+v, missing %v", pairs, want)
		}
	}
}

func TestSwissAvoidsRematchesByBacktracking(t *testing.T) {
	ids := users(4)
	players := contenders(ids)
	players[0].HalfPoints, players[1].HalfPoints = 2, 2
	players[0].Opponents, players[1].Opponents = []uuid.UUID{ids[1], ids[2]}, []uuid.UUID{ids[0], ids[3]}
	players[2].Opponents, players[3].Opponents = []uuid.UUID{ids[0]}, []uuid.UUID{ids[1]}
	pairs, err := pairing.Swiss(players)
	if err != nil {
		t.Fatal(err)
	}
	got := seats(pairs)
	if len(pairs) != 2 || !got[[2]uuid.UUID{ids[0], ids[3]}] || !got[[2]uuid.UUID{ids[1], ids[2]}] {
		t.Fatalf("pairs = %+v", pairs)
	}
}

func TestSwissByeGoesToTheLowestWithoutOne(t *testing.T) {
	ids := users(5)
	for _, tt := range []struct {
		name   string
		hadBye []int
		want   uuid.UUID
	}{
		{name: "nobody had a bye", want: ids[4]},
		{name: "the last had one", hadBye: []int{4}, want: ids[3]},
		{name: "everyone had one", hadBye: []int{0, 1, 2, 3, 4}, want: ids[4]},
	} {
		t.Run(tt.name, func(t *testing.T) {
			players := contenders(ids)
			for _, i := range tt.hadBye {
				players[i].HadBye = true
			}
			pairs, err := pairing.Swiss(players)
			if err != nil {
				t.Fatal(err)
			}
			last := pairs[len(pairs)-1]
			if len(pairs) != 3 || !last.Bye() || last.Seats[0] != tt.want || last.Board != 3 {
				t.Fatalf("pairs = %+v", pairs)
			}
			for _, p := range pairs[:2] {
				if p.Bye() || p.Seats[0] == tt.want || p.Seats[1] == tt.want {
					t.Fatalf("the bye player also plays: %+v", pairs)
				}
			}
		})
	}
}

func TestSwissBalancesFirstMoves(t *testing.T) {
	ids := users(2)
	players := contenders(ids)
	players[0].Firsts = 2
	pairs, err := pairing.Swiss(players)
	if err != nil {
		t.Fatal(err)
	}
	if pairs[0].Seats != [2]uuid.UUID{ids[1], ids[0]} {
		t.Fatalf("pairs = %+v", pairs)
	}
}

func TestSwissFallsBackToRematchesWhenNoneAreLeft(t *testing.T) {
	ids := users(2)
	players := contenders(ids)
	players[0].Opponents, players[1].Opponents = []uuid.UUID{ids[1]}, []uuid.UUID{ids[0]}
	pairs, err := pairing.Swiss(players)
	if err != nil || len(pairs) != 1 || pairs[0].Seats != [2]uuid.UUID{ids[0], ids[1]} {
		t.Fatalf("pairs = %+v, %v", pairs, err)
	}
	if _, err := pairing.Swiss(players[:1]); !errors.Is(err, pairing.ErrPlayers) {
		t.Fatalf("one player = %v", err)
	}
}

func TestSwissSeriesNeverRepeatsAPairing(t *testing.T) {
	ids := users(8)
	players := contenders(ids)
	index := map[uuid.UUID]int{}
	for i, id := range ids {
		index[id] = i
	}
	met := map[[2]uuid.UUID]bool{}
	for round := 1; round <= 7; round++ {
		pairs, err := pairing.Swiss(players)
		if err != nil {
			t.Fatal(err)
		}
		if len(pairs) != 4 {
			t.Fatalf("round %d has %d boards", round, len(pairs))
		}
		for _, p := range pairs {
			a, b := p.Seats[0], p.Seats[1]
			if met[[2]uuid.UUID{a, b}] {
				t.Fatalf("round %d repeats %v", round, p.Seats)
			}
			met[[2]uuid.UUID{a, b}], met[[2]uuid.UUID{b, a}] = true, true
			winner := a
			if index[b] < index[a] {
				winner = b
			}
			players[index[winner]].HalfPoints += 2
			players[index[a]].Firsts++
			players[index[a]].Opponents = append(players[index[a]].Opponents, b)
			players[index[b]].Opponents = append(players[index[b]].Opponents, a)
		}
	}
}
