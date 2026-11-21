package engine

import (
	"encoding/json"
	"slices"
)

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

	Seed uint64 `json:"seed,omitempty,string"`

	Data json.RawMessage `json:"data"`

	MaxPlies int `json:"max_plies,omitempty"`

	MoveLimit bool `json:"move_limit,omitempty"`

	Places []int `json:"places,omitempty"`
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
	if s.MaxPlies > 0 && s.Ply >= s.MaxPlies {
		s = s.Finish(NoSeat)
		s.MoveLimit = true
	}
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
	s.MoveLimit = false
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

func (s State) Rank(places []int) State {
	winner := NoSeat
	for seat, place := range places {
		if place != 1 {
			continue
		}
		if winner != NoSeat {
			winner = NoSeat
			break
		}
		winner = seat
	}
	s = s.Finish(winner)
	if winner == NoSeat && slices.ContainsFunc(places, func(place int) bool { return place > 1 }) {
		s.Status = StatusWin
	}
	s.Places = slices.Clone(places)
	return s
}

type Move struct {
	Player PlayerID        `json:"player"`
	Ply    int             `json:"ply"`
	Data   json.RawMessage `json:"data"`
}
