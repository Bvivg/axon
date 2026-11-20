package saga

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/tournament/internal/pairing"
	"github.com/bvivg/axon/core/shared/pkg/tournamentid"
)

type Effect interface{ effect() }

type RoundStarted struct {
	Round     int
	Game      string
	Matches   []Match
	StartedAt time.Time
}

type ForfeitIssued struct {
	Match Match
	Game  string
	Due   time.Time
}

type Cancellation struct {
	Started  bool
	Sessions []uuid.UUID
	Returned []uuid.UUID
	At       time.Time
}

type Completion struct {
	Winner    uuid.UUID
	Game      string
	Standings []Standing
	At        time.Time
}

func (RoundStarted) effect()  {}
func (ForfeitIssued) effect() {}
func (Cancellation) effect()  {}
func (Completion) effect()    {}

func (t Tournament) Seed(now time.Time) (Tournament, []Effect, error) {
	if t.Status != Seeding {
		return t, nil, nil
	}
	players := t.Registered()
	if len(players) < MinPlayers {
		return t, nil, fmt.Errorf("%w: %d registered", ErrTooFew, len(players))
	}
	slices.SortStableFunc(players, func(a, b Player) int {
		return cmp.Or(cmp.Compare(b.Rating, a.Rating), a.RegisteredAt.Compare(b.RegisteredAt), slices.Compare(a.User[:], b.User[:]))
	})
	seeds := make(map[uuid.UUID]int, len(players))
	for i, p := range players {
		seeds[p.User] = i + 1
	}
	next := t.clone()
	for i, p := range next.Players {
		next.Players[i].Seed = seeds[p.User]
	}
	var pairs []pairing.Pair
	var err error
	switch next.Spec.Format {
	case SingleElimination:
		next.TotalRounds = pairing.Rounds(len(players))
		entrants := make([]pairing.Entrant, len(players))
		for i, p := range players {
			entrants[i] = pairing.Entrant{User: p.User, Seed: i + 1}
		}
		pairs, err = pairing.Bracket(entrants)
	default:
		next.TotalRounds = min(next.Spec.Rounds, len(players)-1)
		pairs, err = pairing.Swiss(next.contenders())
	}
	if err != nil {
		return t, nil, err
	}
	next.Status = Running
	return next.open(1, pairs, now)
}

func (t Tournament) open(round int, pairs []pairing.Pair, now time.Time) (Tournament, []Effect, error) {
	t.Round = round
	t.Rounds = append(t.Rounds, Round{Number: round, StartedAt: now})
	started := RoundStarted{Round: round, Game: t.Spec.Game, StartedAt: now}
	for _, pair := range pairs {
		m := Match{Round: round, Board: pair.Board, Seats: pair.Seats, Result: Pending}
		if pair.Bye() {
			m.Result, m.Winner, m.FinishedAt = Bye, pair.Seats[0], &now
		} else {
			m.Session = tournamentid.Session(t.ID, round, pair.Board)
			started.Matches = append(started.Matches, m)
		}
		t.Matches = append(t.Matches, m)
	}
	if t.RoundComplete() {
		t.Rounds[len(t.Rounds)-1].FinishedAt = &now
	}
	return t, []Effect{started}, nil
}

func (t Tournament) Advance(now time.Time) (Tournament, []Effect, error) {
	switch {
	case t.Status == Seeding:
		return t.Seed(now)
	case t.Status != Running || !t.RoundComplete():
		return t, nil, nil
	}
	next := t.clone()
	if next.Spec.Format == SingleElimination {
		winners := next.winners(next.Round)
		if len(winners) == 1 {
			return next.finish(winners[0].User, now)
		}
		pairs, err := pairing.Advance(winners)
		if err != nil {
			return t, nil, err
		}
		return next.open(next.Round+1, pairs, now)
	}
	if next.Round >= next.TotalRounds {
		return next.finish(next.Standings()[0].User, now)
	}
	pairs, err := pairing.Swiss(next.contenders())
	if err != nil {
		return t, nil, err
	}
	return next.open(next.Round+1, pairs, now)
}

func (t Tournament) finish(winner uuid.UUID, now time.Time) (Tournament, []Effect, error) {
	t.Status, t.Winner, t.FinishedAt = Finished, winner, &now
	return t, []Effect{Completion{Winner: winner, Game: t.Spec.Game, Standings: t.Standings(), At: now}}, nil
}

func (t Tournament) seed(user uuid.UUID) int {
	for _, p := range t.Players {
		if p.User == user {
			return p.Seed
		}
	}
	return 0
}

func (t Tournament) winners(round int) []pairing.Entrant {
	matches := t.RoundMatches(round)
	slices.SortFunc(matches, func(a, b Match) int { return cmp.Compare(a.Board, b.Board) })
	out := make([]pairing.Entrant, 0, len(matches))
	for _, m := range matches {
		out = append(out, pairing.Entrant{User: m.Winner, Seed: t.seed(m.Winner)})
	}
	return out
}

func (t Tournament) contenders() []pairing.Contender {
	byUser := map[uuid.UUID]*pairing.Contender{}
	var order []uuid.UUID
	for _, p := range t.Registered() {
		byUser[p.User] = &pairing.Contender{User: p.User, Seed: p.Seed, Rating: p.Rating}
		order = append(order, p.User)
	}
	for _, s := range t.Standings() {
		if c := byUser[s.User]; c != nil {
			c.HalfPoints, c.HadBye = s.HalfPoints, s.Byes > 0
		}
	}
	for _, m := range t.Matches {
		if m.Result == Aborted {
			continue
		}
		if m.Bye() {
			continue
		}
		a, b := byUser[m.Seats[0]], byUser[m.Seats[1]]
		if a == nil || b == nil {
			continue
		}
		a.Firsts++
		a.Opponents = append(a.Opponents, b.User)
		b.Opponents = append(b.Opponents, a.User)
	}
	out := make([]pairing.Contender, 0, len(order))
	for _, user := range order {
		out = append(out, *byUser[user])
	}
	return out
}

type Standing struct {
	Rank       int
	User       uuid.UUID
	Seed       int
	Rating     int
	HalfPoints int
	Wins       int
	Draws      int
	Losses     int
	Byes       int
	Reach      int
	Eliminated bool
}

func (t Tournament) Standings() []Standing {
	if t.Round == 0 {
		return nil
	}
	rows := map[uuid.UUID]*Standing{}
	var order []uuid.UUID
	for _, p := range t.Registered() {
		rows[p.User] = &Standing{User: p.User, Seed: p.Seed, Rating: p.Rating, Reach: t.Round}
		order = append(order, p.User)
	}
	for _, m := range t.Matches {
		a, b := rows[m.Seats[0]], rows[m.Seats[1]]
		switch m.Result {
		case Bye:
			if a != nil {
				a.Byes++
				a.HalfPoints += WinHalfPoints
			}
		case Win, Draw:
			if a == nil || b == nil {
				continue
			}
			loser := b
			if m.Winner == m.Seats[1] {
				loser = a
			}
			if m.Result == Draw {
				a.Draws++
				b.Draws++
				a.HalfPoints += DrawHalfPoints
				b.HalfPoints += DrawHalfPoints
			} else {
				winner := a
				if loser == a {
					winner = b
				}
				winner.Wins++
				winner.HalfPoints += WinHalfPoints
				loser.Losses++
			}
			if t.Spec.Format == SingleElimination && m.Winner != uuid.Nil {
				loser.Eliminated, loser.Reach = true, m.Round
			}
		}
	}
	if t.Status == Finished && t.Spec.Format == SingleElimination {
		if champion := rows[t.Winner]; champion != nil {
			champion.Reach = t.TotalRounds + 1
		}
	}
	out := make([]Standing, 0, len(order))
	for _, user := range order {
		out = append(out, *rows[user])
	}
	if t.Spec.Format == SingleElimination {
		slices.SortStableFunc(out, func(a, b Standing) int { return cmp.Or(cmp.Compare(b.Reach, a.Reach), cmp.Compare(a.Seed, b.Seed)) })
		for i := range out {
			ahead := 0
			for _, other := range out {
				if other.Reach > out[i].Reach {
					ahead++
				}
			}
			out[i].Rank = ahead + 1
		}
		return out
	}
	slices.SortStableFunc(out, func(a, b Standing) int {
		return cmp.Or(cmp.Compare(b.HalfPoints, a.HalfPoints), cmp.Compare(b.Rating, a.Rating), cmp.Compare(a.Seed, b.Seed))
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}
