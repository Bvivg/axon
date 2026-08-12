package engine

import (
	"encoding/json"
	"fmt"
)

type PlayerID string

type Key string

type Game interface {
	Init(players []PlayerID) State

	ApplyMove(state State, move Move) (State, error)

	ValidMoves(state State, player PlayerID) []Move

	IsTerminal(state State) (bool, *PlayerID)
}

func Precheck(game Key, state State, move Move) (int, error) {
	if state.Game != game {
		return NoSeat, fmt.Errorf("%w: state holds %q, game is %q", ErrGameMismatch, state.Game, game)
	}
	if state.Over() {
		return NoSeat, fmt.Errorf("%w: status %q", ErrGameOver, state.Status)
	}

	seat, ok := state.Seat(move.Player)
	if !ok {
		return NoSeat, fmt.Errorf("%w: %q", ErrPlayerNotInGame, move.Player)
	}
	if seat != state.Turn {
		return NoSeat, fmt.Errorf("%w: seat %d moved, seat %d is to move", ErrOutOfTurn, seat, state.Turn)
	}
	if move.Ply != state.Ply {
		return NoSeat, fmt.Errorf("%w: move made at ply %d, position is at ply %d", ErrStaleMove, move.Ply, state.Ply)
	}

	return seat, nil
}

func DecodeState(state State, dst any) error {
	if err := json.Unmarshal(state.Data, dst); err != nil {
		return fmt.Errorf("%w: %w", ErrStateMalformed, err)
	}
	return nil
}

func DecodeMove(move Move, dst any) error {
	if err := json.Unmarshal(move.Data, dst); err != nil {
		return fmt.Errorf("%w: %w", ErrMoveMalformed, err)
	}
	return nil
}
