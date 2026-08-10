package engine

import "encoding/json"

// Status is the coarse outcome of a position, the one thing about a game's
// progress that every layer above understands.
//
// It is a string rather than an integer enum because it is read where a number
// would need a lookup table: in jsonb columns, in logs, and in the TypeScript
// client. The proto contract mirrors it as a string for the same reason.
type Status string

const (
	// StatusInProgress means moves are still expected.
	StatusInProgress Status = "in_progress"

	// StatusWin means one seat won; State.Winner names it.
	StatusWin Status = "win"

	// StatusDraw means the game ended with no winner.
	StatusDraw Status = "draw"
)

// NoSeat is the seat index used where no seat applies: the winner of a drawn
// game, and the turn of a finished one.
const NoSeat = -1

// State is one position of one game.
//
// Every field except Data is game-agnostic and is what the session, the
// transport and the client are allowed to read. Data is the position itself in
// whatever shape the game chose, encoded as JSON so that the whole State
// marshals into a single readable document — which is exactly how it is stored
// (jsonb) and shipped.
//
// State is a value and is treated as immutable: methods return modified copies
// and never write through Players or Data. That is what makes ApplyMove safe to
// call on a cached position without cloning it first.
type State struct {
	// Game identifies the implementation this position belongs to. It makes a
	// stored position self-describing and lets Precheck refuse a position handed
	// to the wrong game — a mistake JSON decoding would otherwise swallow,
	// silently producing an empty board out of another game's payload.
	Game Key `json:"game"`

	// Players maps seat index to participant. Seats are how the games count;
	// PlayerID is how the platform does.
	Players []PlayerID `json:"players"`

	// Turn is the seat to move, or NoSeat once the game is over. The game sets
	// it: turn order is not always round-robin (in durak one player moves
	// several times running), so it is stored rather than derived.
	Turn int `json:"turn"`

	// Ply counts the moves already applied. It orders the stored history and
	// doubles as the token a move is checked against, so a move computed from a
	// stale position can be refused instead of silently applied.
	Ply int `json:"ply"`

	// Status is the outcome so far.
	Status Status `json:"status"`

	// Winner is the winning seat, or NoSeat when there is none. A seat index
	// rather than a *PlayerID: a pointer inside a value that is copied around
	// would be shared by every copy, and mutating through it would corrupt them
	// all.
	Winner int `json:"winner"`

	// Data is the game-specific payload. The engine never looks inside it.
	Data json.RawMessage `json:"data"`
}

// NewState returns an opening position. Games call it from Init.
//
// The roster is copied: the caller's slice must not become a live handle into a
// value the engine treats as immutable.
func NewState(game Key, players []PlayerID, data json.RawMessage) State {
	roster := make([]PlayerID, len(players))
	copy(roster, players)

	return State{
		Game:    game,
		Players: roster,
		Turn:    0,
		Ply:     0,
		Status:  StatusInProgress,
		Winner:  NoSeat,
		Data:    data,
	}
}

// Over reports whether the game has ended.
func (s State) Over() bool { return s.Status != StatusInProgress }

// Seat returns the seat index of player, and whether they are in this game at
// all.
func (s State) Seat(player PlayerID) (int, bool) {
	for seat, id := range s.Players {
		if id == player {
			return seat, true
		}
	}
	return NoSeat, false
}

// Current returns the player to move. The second result is false when the game
// is over or the position names a seat nobody occupies.
func (s State) Current() (PlayerID, bool) {
	if s.Over() || s.Turn < 0 || s.Turn >= len(s.Players) {
		return "", false
	}
	return s.Players[s.Turn], true
}

// Advance returns the position that follows s: the payload replaced, the turn
// passed to the given seat, the ply counter moved on.
//
// The receiver is a value, so the copy is made by the language rather than by
// hand — s itself cannot be touched here even by accident.
func (s State) Advance(data json.RawMessage, turn int) State {
	s.Data = data
	s.Turn = turn
	s.Ply++
	return s
}

// Finish returns s marked as ended, won by the given seat or drawn when the seat
// is NoSeat.
//
// The turn is cleared as well: a finished game with a seat still "to move" is
// how a client ends up rendering whose turn it is under a final position.
func (s State) Finish(winner int) State {
	if winner == NoSeat {
		s.Status = StatusDraw
	} else {
		s.Status = StatusWin
	}
	s.Winner = winner
	s.Turn = NoSeat
	return s
}

// Outcome reports terminality in the shape Game.IsTerminal returns, so an
// implementation whose ApplyMove already decided the outcome can delegate to it.
// The returned pointer is to a copy, never into the position.
func (s State) Outcome() (bool, *PlayerID) {
	if !s.Over() {
		return false, nil
	}
	if s.Winner < 0 || s.Winner >= len(s.Players) {
		return true, nil
	}

	winner := s.Players[s.Winner]
	return true, &winner
}

// Move is one attempted move.
//
// Player rather than a seat: the client knows who it is, not where it sits, and
// the mapping is the engine's job. Ply is the position the move was computed
// against — see State.Ply.
type Move struct {
	Player PlayerID        `json:"player"`
	Ply    int             `json:"ply"`
	Data   json.RawMessage `json:"data"`
}
