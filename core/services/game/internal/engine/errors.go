package engine

import "errors"

var (
	ErrGameOver = errors.New("engine: game is already over")

	ErrOutOfTurn = errors.New("engine: it is not this player's turn")

	ErrPlayerNotInGame = errors.New("engine: player is not in this game")

	ErrStaleMove = errors.New("engine: move was made against an older position")

	ErrGameMismatch = errors.New("engine: state belongs to a different game")

	ErrStateMalformed = errors.New("engine: state payload is malformed")

	ErrMoveMalformed = errors.New("engine: move payload is malformed")

	ErrUnknownGame = errors.New("engine: no game is registered under this key")

	ErrDuplicateGame = errors.New("engine: game is already registered")

	ErrInvalidDefinition = errors.New("engine: invalid game definition")

	ErrPlayerCount = errors.New("engine: wrong number of players for this game")

	ErrDuplicatePlayer = errors.New("engine: player appears twice in the roster")
)
