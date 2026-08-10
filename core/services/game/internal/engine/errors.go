package engine

import "errors"

// Rules shared by every game fail with these sentinels so callers branch with
// errors.Is instead of matching message text. The server layer maps each one to
// a connect.Code exactly once, in a single place; games add their own sentinels
// for their own rules and wrap them the same way.
var (
	// ErrGameOver is returned for a move against a finished position.
	ErrGameOver = errors.New("engine: game is already over")

	// ErrOutOfTurn is returned when the mover is in the game but it is another
	// seat's turn.
	ErrOutOfTurn = errors.New("engine: it is not this player's turn")

	// ErrPlayerNotInGame is returned when the mover holds no seat at all.
	ErrPlayerNotInGame = errors.New("engine: player is not in this game")

	// ErrStaleMove is returned when a move was computed against an older
	// position than the one it is being applied to. In a strictly alternating
	// game the turn check catches most of these; it does not catch the case that
	// matters, a player who legitimately moves several times running sending the
	// same ply twice.
	ErrStaleMove = errors.New("engine: move was made against an older position")

	// ErrGameMismatch is returned when a position belongs to a different game
	// than the one applying the move.
	ErrGameMismatch = errors.New("engine: state belongs to a different game")

	// ErrStateMalformed is returned when a position's payload cannot be read.
	// It means stored or cached data is corrupt, not that a player did anything
	// wrong.
	ErrStateMalformed = errors.New("engine: state payload is malformed")

	// ErrMoveMalformed is returned when a move's payload cannot be read. Unlike
	// ErrStateMalformed this is routine: the payload comes from a client.
	ErrMoveMalformed = errors.New("engine: move payload is malformed")

	// ErrUnknownGame is returned for a key no game is registered under.
	ErrUnknownGame = errors.New("engine: no game is registered under this key")

	// ErrDuplicateGame is returned when a key is registered twice.
	ErrDuplicateGame = errors.New("engine: game is already registered")

	// ErrInvalidDefinition is returned for a registration that could never
	// produce a playable game.
	ErrInvalidDefinition = errors.New("engine: invalid game definition")

	// ErrPlayerCount is returned when a roster has the wrong number of players
	// for the game being started.
	ErrPlayerCount = errors.New("engine: wrong number of players for this game")

	// ErrDuplicatePlayer is returned when the same player appears twice in a
	// roster. Seat lookup answers with the first match, so the second seat would
	// be unreachable and the game unplayable.
	ErrDuplicatePlayer = errors.New("engine: player appears twice in the roster")
)
