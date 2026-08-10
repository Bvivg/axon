package engine_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

// stubGame is the smallest thing the registry can hold. The registry's job is
// bookkeeping, so testing it against a real game would only add ways for the
// test to fail for reasons that have nothing to do with the registry.
type stubGame struct {
	key engine.Key
}

func (g stubGame) Init(players []engine.PlayerID) engine.State {
	return engine.NewState(g.key, players, json.RawMessage(`{}`))
}

func (stubGame) ApplyMove(state engine.State, _ engine.Move) (engine.State, error) {
	return state, nil
}

func (stubGame) ValidMoves(engine.State, engine.PlayerID) []engine.Move { return nil }

func (stubGame) IsTerminal(state engine.State) (bool, *engine.PlayerID) { return state.Outcome() }

func stubDefinition(key engine.Key, minPlayers, maxPlayers int) engine.Definition {
	return engine.Definition{
		Key:        key,
		MinPlayers: minPlayers,
		MaxPlayers: maxPlayers,
		Game:       stubGame{key: key},
	}
}

func TestRegistryRegister(t *testing.T) {
	tests := []struct {
		name    string
		def     engine.Definition
		wantErr error
	}{
		{
			name: "a playable definition",
			def:  stubDefinition(testKey, 2, 2),
		},
		{
			name: "a definition with a range of rosters",
			def:  stubDefinition("durak", 2, 6),
		},
		{
			name:    "no key",
			def:     stubDefinition("", 2, 2),
			wantErr: engine.ErrInvalidDefinition,
		},
		{
			name:    "no implementation",
			def:     engine.Definition{Key: "chess", MinPlayers: 2, MaxPlayers: 2},
			wantErr: engine.ErrInvalidDefinition,
		},
		{
			name:    "a game nobody can play",
			def:     stubDefinition("solitaire", 0, 0),
			wantErr: engine.ErrInvalidDefinition,
		},
		{
			name:    "a roster range that is inside out",
			def:     stubDefinition("chess", 4, 2),
			wantErr: engine.ErrInvalidDefinition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := engine.NewRegistry()

			err := registry.Register(tt.def)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Register() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}

			if _, err := registry.Get(tt.def.Key); err != nil {
				t.Errorf("Get(%q) after registering it: %v", tt.def.Key, err)
			}
		})
	}
}

func TestRegistryRefusesToReplaceAGame(t *testing.T) {
	registry := engine.NewRegistry()

	if err := registry.Register(stubDefinition(testKey, 2, 2)); err != nil {
		t.Fatalf("first Register(): %v", err)
	}

	// A silent overwrite would mean a build hosting a game nobody wired on
	// purpose, and no way to notice.
	err := registry.Register(stubDefinition(testKey, 2, 4))
	if !errors.Is(err, engine.ErrDuplicateGame) {
		t.Fatalf("second Register() error = %v, want %v", err, engine.ErrDuplicateGame)
	}

	def, err := registry.Definition(testKey)
	if err != nil {
		t.Fatalf("Definition(): %v", err)
	}
	if def.MaxPlayers != 2 {
		t.Errorf("the rejected registration still took effect: MaxPlayers = %d", def.MaxPlayers)
	}
}

func TestRegistryUnknownGame(t *testing.T) {
	registry := engine.NewRegistry()

	if _, err := registry.Get("chess"); !errors.Is(err, engine.ErrUnknownGame) {
		t.Errorf("Get() error = %v, want %v", err, engine.ErrUnknownGame)
	}
	if _, err := registry.Definition("chess"); !errors.Is(err, engine.ErrUnknownGame) {
		t.Errorf("Definition() error = %v, want %v", err, engine.ErrUnknownGame)
	}
	if _, err := registry.NewState("chess", []engine.PlayerID{alice, bob}); !errors.Is(err, engine.ErrUnknownGame) {
		t.Errorf("NewState() error = %v, want %v", err, engine.ErrUnknownGame)
	}
}

func TestRegistryKeysAreStable(t *testing.T) {
	registry := engine.NewRegistry()

	for _, key := range []engine.Key{"quoridor", "chess", "tictactoe"} {
		if err := registry.Register(stubDefinition(key, 2, 2)); err != nil {
			t.Fatalf("Register(%q): %v", key, err)
		}
	}

	want := []engine.Key{"chess", "quoridor", "tictactoe"}
	if got := registry.Keys(); !slices.Equal(got, want) {
		t.Errorf("Keys() = %v, want %v", got, want)
	}

	if got := engine.NewRegistry().Keys(); len(got) != 0 {
		t.Errorf("Keys() on an empty registry = %v, want nothing", got)
	}
}

// Init cannot refuse a roster — the interface gives it no way to — so the check
// lives here, and this is the test that it does.
func TestRegistryNewStateChecksTheRoster(t *testing.T) {
	tests := []struct {
		name    string
		players []engine.PlayerID
		wantErr error
	}{
		{
			name:    "a roster the game allows",
			players: []engine.PlayerID{alice, bob},
		},
		{
			name:    "too few players",
			players: []engine.PlayerID{alice},
			wantErr: engine.ErrPlayerCount,
		},
		{
			name:    "too many players",
			players: []engine.PlayerID{alice, bob, carol},
			wantErr: engine.ErrPlayerCount,
		},
		{
			name:    "nobody at all",
			wantErr: engine.ErrPlayerCount,
		},
		{
			// Seat lookup answers with the first match, so the second seat would
			// be unreachable and the game unplayable.
			name:    "the same player twice",
			players: []engine.PlayerID{alice, alice},
			wantErr: engine.ErrDuplicatePlayer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := engine.NewRegistry()
			if err := registry.Register(stubDefinition(testKey, 2, 2)); err != nil {
				t.Fatalf("Register(): %v", err)
			}

			state, err := registry.NewState(testKey, tt.players)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewState() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}

			if state.Game != testKey {
				t.Errorf("NewState() game = %q, want %q", state.Game, testKey)
			}
			if len(state.Players) != len(tt.players) {
				t.Errorf("NewState() seated %d players, want %d", len(state.Players), len(tt.players))
			}
		})
	}
}
