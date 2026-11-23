package session

import (
	"cmp"
	"encoding/json"
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

var (
	ErrInvalidTable = errors.New("invalid table settings")
	ErrNotHost      = errors.New("only the host can fill the table")
	ErrNoBots       = errors.New("this game has no computer players")
	ErrTwoPlayers   = errors.New("this action needs exactly two players")
)

var botPrefix = [15]byte{0xb0, 0x7b, 0x07, 0x5e, 0xa7, 0x00, 0x40, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

const DefaultSeats = 2

type Table struct {
	Seats   int
	Options json.RawMessage
	Bots    int
}

func (t Table) Plain() bool {
	return t.Seats == 0 && len(t.Options) == 0 && t.Bots == 0
}

func BotID(seat int) uuid.UUID {
	var id uuid.UUID
	copy(id[:], botPrefix[:])
	id[15] = byte(seat)
	return id
}

func IsBot(id uuid.UUID) bool {
	return [15]byte(id[:15]) == botPrefix
}

func (s Session) Capacity() int {
	return cmp.Or(s.Seats, DefaultSeats)
}

func (s Session) Players() []uuid.UUID {
	if len(s.Seated) > 0 {
		return s.Seated
	}
	if s.Opponent == nil {
		return []uuid.UUID{s.Creator}
	}
	return []uuid.UUID{s.Creator, *s.Opponent}
}

func (s Session) Started() bool { return s.Opponent != nil }

func (s Session) Multiseat() bool {
	return len(s.Players()) > DefaultSeats || s.Capacity() > DefaultSeats
}

func (s Session) SeatOf(user uuid.UUID) (int, bool) {
	seat := slices.Index(s.Players(), user)
	return seat, seat >= 0
}

func (s Session) Automated(seat int) bool {
	players := s.Players()
	if seat < 0 || seat >= len(players) {
		return false
	}
	return IsBot(players[seat]) || slices.Contains(s.Left, seat)
}

func (s Session) Out(seat int) bool {
	return seat >= 0 && seat < len(s.State.Places) && s.State.Places[seat] > 0
}

func (s Session) Places() []int {
	if !s.Started() || !s.State.Over() {
		return nil
	}
	n := len(s.Players())
	base := s.State.Places
	if len(base) != n || slices.Contains(base, 0) {
		base = make([]int, n)
		for seat := range base {
			base[seat] = 1
			if s.State.Winner != engine.NoSeat && seat != s.State.Winner {
				base[seat] = 2
			}
		}
	}
	if len(s.Left) == 0 {
		return slices.Clone(base)
	}
	stayers := make([]int, 0, n)
	for seat := range n {
		if !slices.Contains(s.Left, seat) {
			stayers = append(stayers, seat)
		}
	}
	slices.SortStableFunc(stayers, func(a, b int) int { return base[a] - base[b] })
	places := make([]int, n)
	for i, seat := range stayers {
		places[seat] = i + 1
		if i > 0 && base[seat] == base[stayers[i-1]] {
			places[seat] = places[stayers[i-1]]
		}
	}
	for i, seat := range s.Left {
		if seat >= 0 && seat < n {
			places[seat] = n - i
		}
	}
	return places
}
