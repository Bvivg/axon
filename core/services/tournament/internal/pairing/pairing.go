package pairing

import (
	"cmp"
	"errors"
	"fmt"
	"math/bits"
	"slices"

	"github.com/google/uuid"
)

var ErrPlayers = errors.New("pairing: not enough players")

type Entrant struct {
	User uuid.UUID
	Seed int
}

type Pair struct {
	Board int
	Seats [2]uuid.UUID
}

func (p Pair) Bye() bool { return p.Seats[1] == uuid.Nil }

func BracketSize(players int) int {
	if players <= 2 {
		return 2
	}
	return 1 << bits.Len(uint(players-1))
}

func Rounds(players int) int {
	return bits.Len(uint(BracketSize(players))) - 1
}

func SeedOrder(size int) []int {
	order := []int{1, 2}
	for len(order) < size {
		next := make([]int, 0, len(order)*2)
		for _, seed := range order {
			next = append(next, seed, 2*len(order)+1-seed)
		}
		order = next
	}
	return order
}

func Bracket(entrants []Entrant) ([]Pair, error) {
	if len(entrants) < 2 {
		return nil, fmt.Errorf("%w: %d", ErrPlayers, len(entrants))
	}
	bySeed := make(map[int]uuid.UUID, len(entrants))
	for _, e := range entrants {
		if e.Seed < 1 || e.Seed > len(entrants) {
			return nil, fmt.Errorf("pairing: seed %d out of 1..%d", e.Seed, len(entrants))
		}
		if _, dup := bySeed[e.Seed]; dup {
			return nil, fmt.Errorf("pairing: seed %d is taken twice", e.Seed)
		}
		bySeed[e.Seed] = e.User
	}
	order := SeedOrder(BracketSize(len(entrants)))
	pairs := make([]Pair, 0, len(order)/2)
	for i := 0; i < len(order); i += 2 {
		pairs = append(pairs, Pair{Board: i/2 + 1, Seats: [2]uuid.UUID{bySeed[order[i]], bySeed[order[i+1]]}})
	}
	return pairs, nil
}

func Advance(winners []Entrant) ([]Pair, error) {
	if len(winners) < 2 || len(winners)%2 != 0 {
		return nil, fmt.Errorf("%w: %d winners cannot be paired", ErrPlayers, len(winners))
	}
	pairs := make([]Pair, 0, len(winners)/2)
	for i := 0; i < len(winners); i += 2 {
		a, b := winners[i], winners[i+1]
		if b.Seed < a.Seed {
			a, b = b, a
		}
		pairs = append(pairs, Pair{Board: i/2 + 1, Seats: [2]uuid.UUID{a.User, b.User}})
	}
	return pairs, nil
}

type Contender struct {
	User       uuid.UUID
	Seed       int
	Rating     int
	HalfPoints int
	Firsts     int
	HadBye     bool
	Opponents  []uuid.UUID
}

const searchBudget = 200_000

func Rank(players []Contender) []Contender {
	ranked := slices.Clone(players)
	slices.SortStableFunc(ranked, func(a, b Contender) int {
		return cmp.Or(cmp.Compare(b.HalfPoints, a.HalfPoints), cmp.Compare(b.Rating, a.Rating), cmp.Compare(a.Seed, b.Seed))
	})
	return ranked
}

func Swiss(players []Contender) ([]Pair, error) {
	if len(players) < 2 {
		return nil, fmt.Errorf("%w: %d", ErrPlayers, len(players))
	}
	ranked := Rank(players)
	var bye *Contender
	if len(ranked)%2 == 1 {
		pick := len(ranked) - 1
		for i := len(ranked) - 1; i >= 0; i-- {
			if !ranked[i].HadBye {
				pick = i
				break
			}
		}
		chosen := ranked[pick]
		bye = &chosen
		ranked = slices.Delete(ranked, pick, pick+1)
	}
	met := map[[2]uuid.UUID]bool{}
	for _, p := range ranked {
		for _, o := range p.Opponents {
			met[[2]uuid.UUID{p.User, o}] = true
			met[[2]uuid.UUID{o, p.User}] = true
		}
	}
	budget := searchBudget
	matched, ok := match(ranked, met, &budget)
	if !ok {
		matched, _ = match(ranked, map[[2]uuid.UUID]bool{}, nil)
	}
	pairs := make([]Pair, 0, len(matched)+1)
	for i, m := range matched {
		first, second := m[0], m[1]
		if second.Firsts < first.Firsts {
			first, second = second, first
		}
		pairs = append(pairs, Pair{Board: i + 1, Seats: [2]uuid.UUID{first.User, second.User}})
	}
	if bye != nil {
		pairs = append(pairs, Pair{Board: len(pairs) + 1, Seats: [2]uuid.UUID{bye.User, uuid.Nil}})
	}
	return pairs, nil
}

func match(rest []Contender, met map[[2]uuid.UUID]bool, budget *int) ([][2]Contender, bool) {
	if len(rest) == 0 {
		return nil, true
	}
	if budget != nil {
		if *budget <= 0 {
			return nil, false
		}
		*budget--
	}
	first := rest[0]
	for i := 1; i < len(rest); i++ {
		if met[[2]uuid.UUID{first.User, rest[i].User}] {
			continue
		}
		remaining := make([]Contender, 0, len(rest)-2)
		remaining = append(remaining, rest[1:i]...)
		remaining = append(remaining, rest[i+1:]...)
		if tail, ok := match(remaining, met, budget); ok {
			return append([][2]Contender{{first, rest[i]}}, tail...), true
		}
	}
	return nil, false
}
