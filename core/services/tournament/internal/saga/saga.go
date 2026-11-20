package saga

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type Format string

const (
	SingleElimination Format = "single_elimination"
	Swiss             Format = "swiss"
)

type Status string

const (
	Registration Status = "registration"
	Seeding      Status = "seeding"
	Running      Status = "running"
	Finished     Status = "finished"
	Cancelled    Status = "cancelled"
)

type Result string

const (
	Pending Result = "pending"
	Win     Result = "win"
	Draw    Result = "draw"
	Bye     Result = "bye"
	Aborted Result = "aborted"
)

const (
	MaxNameLength   = 64
	MaxSwissPlayers = 64
	MaxSwissRounds  = 9
	MinPlayers      = 2
	WinHalfPoints   = 2
	DrawHalfPoints  = 1
)

var (
	ErrInvalid      = errors.New("saga: invalid tournament")
	ErrClosed       = errors.New("saga: registration is closed")
	ErrFull         = errors.New("saga: the tournament is full")
	ErrTooFew       = errors.New("saga: not enough players to start")
	ErrNotRunning   = errors.New("saga: the tournament is not running")
	ErrFinal        = errors.New("saga: the tournament is already over")
	ErrUnknownMatch = errors.New("saga: no match plays this session")
	ErrMismatch     = errors.New("saga: the game was played by other players")
)

var EliminationSizes = []int{4, 8, 16}

type Spec struct {
	Name     string
	Game     string
	Format   Format
	Capacity int
	Rounds   int
}

type Player struct {
	User         uuid.UUID
	Rating       int
	Seed         int
	RegisteredAt time.Time
	Returned     bool
}

type Match struct {
	Round      int
	Board      int
	Session    uuid.UUID
	Seats      [2]uuid.UUID
	Result     Result
	Winner     uuid.UUID
	Forfeit    bool
	FinishedAt *time.Time
}

func (m Match) Bye() bool { return m.Seats[1] == uuid.Nil }

func (m Match) Decided() bool { return m.Result != Pending }

type Round struct {
	Number     int
	StartedAt  time.Time
	FinishedAt *time.Time
}

type Tournament struct {
	ID          uuid.UUID
	Creator     uuid.UUID
	Spec        Spec
	TotalRounds int
	Status      Status
	Round       int
	Winner      uuid.UUID
	Players     []Player
	Rounds      []Round
	Matches     []Match
	CreatedAt   time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
	CancelledAt *time.Time
}

func ValidSpec(spec Spec) (Spec, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	switch {
	case spec.Name == "" || utf8.RuneCountInString(spec.Name) > MaxNameLength:
		return Spec{}, fmt.Errorf("%w: the name must be 1 to %d characters", ErrInvalid, MaxNameLength)
	case spec.Game == "":
		return Spec{}, fmt.Errorf("%w: no game", ErrInvalid)
	}
	switch spec.Format {
	case SingleElimination:
		if !slices.Contains(EliminationSizes, spec.Capacity) {
			return Spec{}, fmt.Errorf("%w: a bracket holds 4, 8 or 16 players", ErrInvalid)
		}
		spec.Rounds = 0
	case Swiss:
		if spec.Capacity < MinPlayers+1 || spec.Capacity > MaxSwissPlayers {
			return Spec{}, fmt.Errorf("%w: a swiss tournament holds 3 to %d players", ErrInvalid, MaxSwissPlayers)
		}
		if spec.Rounds < 1 || spec.Rounds > MaxSwissRounds || spec.Rounds >= spec.Capacity {
			return Spec{}, fmt.Errorf("%w: a swiss tournament plays 1 to %d rounds, fewer than its players", ErrInvalid, MaxSwissRounds)
		}
	default:
		return Spec{}, fmt.Errorf("%w: format %q", ErrInvalid, spec.Format)
	}
	return spec, nil
}

func Create(id, creator uuid.UUID, spec Spec, now time.Time) (Tournament, error) {
	valid, err := ValidSpec(spec)
	if err != nil {
		return Tournament{}, err
	}
	if id == uuid.Nil || creator == uuid.Nil {
		return Tournament{}, fmt.Errorf("%w: missing id or creator", ErrInvalid)
	}
	return Tournament{ID: id, Creator: creator, Spec: valid, Status: Registration, TotalRounds: valid.Rounds, CreatedAt: now}, nil
}

func (t Tournament) Started() bool {
	return t.StartedAt != nil
}

func (t Tournament) Over() bool {
	return t.Status == Finished || t.Status == Cancelled
}

func (t Tournament) Registered() []Player {
	out := make([]Player, 0, len(t.Players))
	for _, p := range t.Players {
		if !p.Returned {
			out = append(out, p)
		}
	}
	return out
}

func (t Tournament) Has(user uuid.UUID) bool {
	return slices.ContainsFunc(t.Registered(), func(p Player) bool { return p.User == user })
}

func (t Tournament) clone() Tournament {
	t.Players = slices.Clone(t.Players)
	t.Rounds = slices.Clone(t.Rounds)
	t.Matches = slices.Clone(t.Matches)
	return t
}

func (t Tournament) Register(user uuid.UUID, rating int, now time.Time) (Tournament, bool, error) {
	if t.Status != Registration {
		return t, false, ErrClosed
	}
	if t.Has(user) {
		return t, false, nil
	}
	if len(t.Registered()) >= t.Spec.Capacity {
		return t, false, ErrFull
	}
	next := t.clone()
	next.Players = append(next.Players, Player{User: user, Rating: rating, RegisteredAt: now})
	return next, true, nil
}

func (t Tournament) Withdraw(user uuid.UUID) (Tournament, bool, error) {
	if t.Status != Registration {
		return t, false, ErrClosed
	}
	if !t.Has(user) {
		return t, false, nil
	}
	next := t.clone()
	next.Players = slices.DeleteFunc(next.Players, func(p Player) bool { return p.User == user })
	return next, true, nil
}

func (t Tournament) Start(now time.Time) (Tournament, bool, error) {
	switch t.Status {
	case Seeding, Running:
		return t, false, nil
	case Finished, Cancelled:
		return t, false, ErrFinal
	}
	if len(t.Registered()) < MinPlayers {
		return t, false, fmt.Errorf("%w: %d registered", ErrTooFew, len(t.Registered()))
	}
	next := t.clone()
	next.Status, next.StartedAt = Seeding, &now
	return next, true, nil
}

func (t Tournament) Cancel(now time.Time) (Tournament, Effect, error) {
	if t.Over() {
		return t, nil, ErrFinal
	}
	next := t.clone()
	next.Status, next.CancelledAt = Cancelled, &now
	effect := Cancellation{Started: t.Started(), At: now}
	if !t.Started() {
		for i := range next.Players {
			next.Players[i].Returned = true
			effect.Returned = append(effect.Returned, next.Players[i].User)
		}
		return next, effect, nil
	}
	for i, m := range next.Matches {
		if m.Decided() {
			continue
		}
		next.Matches[i].Result, next.Matches[i].FinishedAt = Aborted, &now
		effect.Sessions = append(effect.Sessions, m.Session)
	}
	for i, r := range next.Rounds {
		if r.FinishedAt == nil {
			next.Rounds[i].FinishedAt = &now
		}
	}
	return next, effect, nil
}

func (t Tournament) match(session uuid.UUID) (int, bool) {
	for i, m := range t.Matches {
		if m.Session == session && session != uuid.Nil {
			return i, true
		}
	}
	return 0, false
}

func (t Tournament) Match(session uuid.UUID) (Match, bool) {
	i, ok := t.match(session)
	if !ok {
		return Match{}, false
	}
	return t.Matches[i], true
}

func (t Tournament) RoundMatches(round int) []Match {
	var out []Match
	for _, m := range t.Matches {
		if m.Round == round {
			out = append(out, m)
		}
	}
	return out
}

func (t Tournament) RoundComplete() bool {
	matches := t.RoundMatches(t.Round)
	return t.Round > 0 && len(matches) > 0 && !slices.ContainsFunc(matches, func(m Match) bool { return !m.Decided() })
}

type Outcome struct {
	Players [2]uuid.UUID
	Winner  int
	Plies   int
	At      time.Time
}

const NoWinner = -1

func (t Tournament) Record(session uuid.UUID, outcome Outcome) (Tournament, bool, error) {
	i, ok := t.match(session)
	if !ok {
		return t, false, ErrUnknownMatch
	}
	if t.Status != Running {
		return t, false, ErrNotRunning
	}
	m := t.Matches[i]
	if m.Decided() {
		return t, false, nil
	}
	if outcome.Players != m.Seats {
		return t, false, fmt.Errorf("%w: board %d of round %d", ErrMismatch, m.Board, m.Round)
	}
	at := outcome.At
	switch outcome.Winner {
	case 0, 1:
		m.Result, m.Winner = Win, m.Seats[outcome.Winner]
		m.Forfeit = outcome.Plies < 2
	case NoWinner:
		m.Result = Draw
		if t.Spec.Format == SingleElimination {
			m.Winner = m.Seats[0]
		}
	default:
		return t, false, fmt.Errorf("%w: winner seat %d", ErrMismatch, outcome.Winner)
	}
	m.FinishedAt = &at
	next := t.clone()
	next.Matches[i] = m
	if m.Round == next.Round && next.RoundComplete() {
		for r := range next.Rounds {
			if next.Rounds[r].Number == next.Round && next.Rounds[r].FinishedAt == nil {
				next.Rounds[r].FinishedAt = &at
			}
		}
	}
	return next, true, nil
}

func (t Tournament) CurrentRound() (Round, bool) {
	for _, r := range t.Rounds {
		if r.Number == t.Round {
			return r, true
		}
	}
	return Round{}, false
}

func (t Tournament) Forfeit(session uuid.UUID, due time.Time) (Effect, bool) {
	if t.Status != Running {
		return nil, false
	}
	m, ok := t.Match(session)
	if !ok || m.Decided() || m.Bye() {
		return nil, false
	}
	return ForfeitIssued{Match: m, Game: t.Spec.Game, Due: due}, true
}
