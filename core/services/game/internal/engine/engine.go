// Package engine defines the contract every game in this service implements and
// the envelope its positions and moves travel in.
//
// The envelope is the point of the package. A game service that only ever hosted
// tic-tac-toe would model the board directly; this one has to store, replay and
// ship positions for games as unlike each other as chess and Quoridor without
// the storage, the transport or the contract learning anything about them. So
// State and Move carry two parts: a skeleton that is identical for every game —
// who is playing, whose turn it is, how far along the game is, whether it is
// over — and one opaque field holding whatever only the game itself understands.
//
// Everything above this package reads the skeleton and passes the payload
// through untouched. Everything below — a Game implementation — reads the
// payload and is handed its seat by Precheck. Adding a game therefore changes no
// storage schema, no contract and no session code, which is the architectural
// claim the whole service exists to demonstrate.
package engine

import (
	"encoding/json"
	"fmt"
)

// PlayerID is a participant as the rest of the platform knows them: the auth
// service's user id. The engine never interprets it, it only matches it against
// the roster in State.Players to find the player's seat.
type PlayerID string

// Key names a game implementation in a Registry, for example "tictactoe". It is
// stored inside State and therefore ends up in Postgres and on the wire: values
// are part of the persisted contract and are never renamed, only added.
type Key string

// Game is the single interface every game implements. Nothing may be added to
// it: the layers above hold games only as this interface, and a method that
// exists on one game and not another destroys that interchangeability.
//
// All four methods are pure functions of their arguments. In particular
// ApplyMove performs no I/O and does not modify the state it is given — it
// returns a new one — because the session layer replays stored move histories
// through it and caches the results.
type Game interface {
	// Init returns the opening position for the given roster. It cannot fail,
	// so it trusts the roster; Registry.NewState is the entry point that
	// validates one first.
	Init(players []PlayerID) State

	// ApplyMove returns the position that follows state once move is played, or
	// an error explaining why the move is illegal.
	ApplyMove(state State, move Move) (State, error)

	// ValidMoves lists the moves player may play right now, ready to be handed
	// back to ApplyMove unchanged. It returns nothing when it is not the
	// player's turn or the game is over.
	ValidMoves(state State, player PlayerID) []Move

	// IsTerminal reports whether the game has ended and who won. A finished game
	// with no winner — a draw — reports true and a nil player.
	IsTerminal(state State) (bool, *PlayerID)
}

// Precheck applies the rules that hold in every game and returns the seat of the
// player making the move.
//
// It is the first statement of every ApplyMove implementation. Keeping these
// checks here rather than in each game is what stops them from drifting apart;
// letting the game call them, rather than wrapping games in a decorator that
// checks first, keeps a direct call and a call through the Registry equivalent.
//
// The order is deliberate: who before when. A player sending a move out of turn
// should hear about the turn, not about a ply number they never see.
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

// DecodeState unmarshals the game-specific payload of state into dst.
//
// A failure here is not a player error but a corrupt or mismatched position, so
// it is reported as its own sentinel: the transport layer owes the client a
// different answer for "your move is illegal" than for "this saved game cannot
// be read".
func DecodeState(state State, dst any) error {
	if err := json.Unmarshal(state.Data, dst); err != nil {
		return fmt.Errorf("%w: %w", ErrStateMalformed, err)
	}
	return nil
}

// DecodeMove unmarshals the game-specific payload of move into dst. Unlike
// DecodeState, a failure here is ordinary: the payload comes straight from a
// client and may be anything at all.
func DecodeMove(move Move, dst any) error {
	if err := json.Unmarshal(move.Data, dst); err != nil {
		return fmt.Errorf("%w: %w", ErrMoveMalformed, err)
	}
	return nil
}
