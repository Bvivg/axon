package engine

import (
	"fmt"
	"slices"
)

// Definition is a game plus the little the layers above must know before they
// can start one.
//
// The seat counts live here rather than on Game because Init cannot fail: the
// fixed interface has no way to refuse a roster, and widening it to say so would
// break the interchangeability the interface exists for. Registry.NewState does
// the refusing instead.
type Definition struct {
	// Key is how the game is addressed and how a stored session names it.
	Key Key

	// MinPlayers and MaxPlayers bound the roster. They are separate numbers
	// because several games in the plan take a range — durak two to six,
	// Quoridor two or four — and tic-tac-toe simply sets both to 2.
	MinPlayers int
	MaxPlayers int

	// Game is the implementation.
	Game Game
}

// Registry maps keys to game implementations and is the only place that knows
// which games this build hosts.
//
// It is filled once during wiring, from a single goroutine, and is read-only
// afterwards — no locking, and no init()-time self-registration either: games
// that register themselves as an import side effect make the contents depend on
// the import graph and make tests share one global.
type Registry struct {
	definitions map[Key]Definition
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{definitions: make(map[Key]Definition)}
}

// Register adds a game. It rejects a definition that could not produce a
// playable game, and refuses to replace an existing key: a silent overwrite
// would mean a build hosting a game nobody wired on purpose.
func (r *Registry) Register(def Definition) error {
	switch {
	case def.Key == "":
		return fmt.Errorf("%w: empty key", ErrInvalidDefinition)
	case def.Game == nil:
		return fmt.Errorf("%w: %q has no implementation", ErrInvalidDefinition, def.Key)
	case def.MinPlayers < 1:
		return fmt.Errorf("%w: %q allows %d players", ErrInvalidDefinition, def.Key, def.MinPlayers)
	case def.MaxPlayers < def.MinPlayers:
		return fmt.Errorf("%w: %q allows %d..%d players", ErrInvalidDefinition, def.Key, def.MinPlayers, def.MaxPlayers)
	}

	if _, exists := r.definitions[def.Key]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateGame, def.Key)
	}

	r.definitions[def.Key] = def
	return nil
}

// Get returns the implementation registered under key.
func (r *Registry) Get(key Key) (Game, error) {
	def, err := r.Definition(key)
	if err != nil {
		return nil, err
	}
	return def.Game, nil
}

// Definition returns the full registration for key.
func (r *Registry) Definition(key Key) (Definition, error) {
	def, ok := r.definitions[key]
	if !ok {
		return Definition{}, fmt.Errorf("%w: %q", ErrUnknownGame, key)
	}
	return def, nil
}

// Keys lists the registered games in a stable order, for listing endpoints and
// per-game metrics.
func (r *Registry) Keys() []Key {
	keys := make([]Key, 0, len(r.definitions))
	for key := range r.definitions {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// NewState starts a game: it validates the roster and then calls Init. This is
// the entry point the session layer uses — calling a game's Init directly skips
// the only roster check there is.
func (r *Registry) NewState(key Key, players []PlayerID) (State, error) {
	def, err := r.Definition(key)
	if err != nil {
		return State{}, err
	}

	if len(players) < def.MinPlayers || len(players) > def.MaxPlayers {
		return State{}, fmt.Errorf("%w: %q takes %d..%d, got %d",
			ErrPlayerCount, key, def.MinPlayers, def.MaxPlayers, len(players))
	}

	seen := make(map[PlayerID]struct{}, len(players))
	for _, player := range players {
		if _, duplicate := seen[player]; duplicate {
			return State{}, fmt.Errorf("%w: %q", ErrDuplicatePlayer, player)
		}
		seen[player] = struct{}{}
	}

	return def.Game.Init(players), nil
}
