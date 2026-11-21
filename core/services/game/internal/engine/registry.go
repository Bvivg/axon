package engine

import (
	"fmt"
	"slices"
	"time"
)

type Definition struct {
	Key Key

	MinPlayers int
	MaxPlayers int

	Game Game

	TimeLimit time.Duration

	MaxPlies int
}

const DefaultTimeLimit = 5 * time.Minute

type Registry struct {
	definitions map[Key]Definition
}

func NewRegistry() *Registry {
	return &Registry{definitions: make(map[Key]Definition)}
}

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
	case def.MaxPlies < 0:
		return fmt.Errorf("%w: %q allows %d plies", ErrInvalidDefinition, def.Key, def.MaxPlies)
	}

	if _, exists := r.definitions[def.Key]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateGame, def.Key)
	}

	r.definitions[def.Key] = def
	return nil
}

func (r *Registry) Get(key Key) (Game, error) {
	def, err := r.Definition(key)
	if err != nil {
		return nil, err
	}
	return def.Game, nil
}

func (r *Registry) Definition(key Key) (Definition, error) {
	def, ok := r.definitions[key]
	if !ok {
		return Definition{}, fmt.Errorf("%w: %q", ErrUnknownGame, key)
	}
	return def, nil
}

func (r *Registry) TimeLimit(key Key) time.Duration {
	if def, ok := r.definitions[key]; ok && def.TimeLimit > 0 {
		return def.TimeLimit
	}
	return DefaultTimeLimit
}

func (r *Registry) Keys() []Key {
	keys := make([]Key, 0, len(r.definitions))
	for key := range r.definitions {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func (r *Registry) NewState(key Key, players []PlayerID) (State, error) {
	def, err := r.roster(key, players)
	if err != nil {
		return State{}, err
	}
	if _, seeded := def.Game.(Seeded); seeded {
		return State{}, fmt.Errorf("%w: %q", ErrSeedRequired, key)
	}
	state := def.Game.Init(players)
	state.MaxPlies = def.MaxPlies
	return state, nil
}

func (r *Registry) NewSeededState(key Key, players []PlayerID, seed uint64) (State, error) {
	def, err := r.roster(key, players)
	if err != nil {
		return State{}, err
	}
	game, seeded := def.Game.(Seeded)
	if !seeded {
		state := def.Game.Init(players)
		state.MaxPlies = def.MaxPlies
		return state, nil
	}
	state := game.InitSeeded(players, seed)
	state.Seed = seed
	state.MaxPlies = def.MaxPlies
	return state, nil
}

func (r *Registry) Hidden(key Key) bool {
	def, ok := r.definitions[key]
	if !ok {
		return false
	}
	_, hidden := def.Game.(Viewer)
	return hidden
}

func (r *Registry) View(state State, viewer PlayerID) State {
	state.Seed = 0
	def, ok := r.definitions[state.Game]
	if !ok {
		return state
	}
	if game, hidden := def.Game.(Viewer); hidden {
		return game.View(state, viewer)
	}
	return state
}

func (r *Registry) roster(key Key, players []PlayerID) (Definition, error) {
	def, err := r.Definition(key)
	if err != nil {
		return Definition{}, err
	}

	if len(players) < def.MinPlayers || len(players) > def.MaxPlayers {
		return Definition{}, fmt.Errorf("%w: %q takes %d..%d, got %d",
			ErrPlayerCount, key, def.MinPlayers, def.MaxPlayers, len(players))
	}

	seen := make(map[PlayerID]struct{}, len(players))
	for _, player := range players {
		if _, duplicate := seen[player]; duplicate {
			return Definition{}, fmt.Errorf("%w: %q", ErrDuplicatePlayer, player)
		}
		seen[player] = struct{}{}
	}
	return def, nil
}
