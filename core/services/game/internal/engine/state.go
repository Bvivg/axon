package engine

import "encoding/json"

type Status string

const (
	StatusInProgress Status = "in_progress"

	StatusWin Status = "win"

	StatusDraw Status = "draw"
)

const NoSeat = -1

type State struct {
	Game Key `json:"game"`

	Players []PlayerID `json:"players"`

	Turn int `json:"turn"`

	Ply int `json:"ply"`

	Status Status `json:"status"`

	Winner int `json:"winner"`

	Data json.RawMessage `json:"data"`
}

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

func (s State) Over() bool { return s.Status != StatusInProgress }

func (s State) Seat(player PlayerID) (int, bool) {
	for seat, id := range s.Players {
		if id == player {
			return seat, true
		}
	}
	return NoSeat, false
}

func (s State) Current() (PlayerID, bool) {
	if s.Over() || s.Turn < 0 || s.Turn >= len(s.Players) {
		return "", false
	}
	return s.Players[s.Turn], true
}

func (s State) Advance(data json.RawMessage, turn int) State {
	s.Data = data
	s.Turn = turn
	s.Ply++
	return s
}

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

type Move struct {
	Player PlayerID        `json:"player"`
	Ply    int             `json:"ply"`
	Data   json.RawMessage `json:"data"`
}
