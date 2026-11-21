package engine_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

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

type hiddenGame struct {
	stubGame
}

func (g hiddenGame) InitSeeded(players []engine.PlayerID, seed uint64) engine.State {
	raw, _ := json.Marshal(map[string]any{"deck": seed % 97, "hands": players})
	return engine.NewState(g.key, players, raw)
}

func (hiddenGame) View(state engine.State, viewer engine.PlayerID) engine.State {
	raw, _ := json.Marshal(map[string]any{"viewer": viewer})
	state.Data = raw
	return state
}

func TestRegistrySeedsAndViewsOptionalExtensions(t *testing.T) {
	games := engine.NewRegistry()
	for _, def := range []engine.Definition{stubDefinition(testKey, 2, 2), {Key: "cards", MinPlayers: 2, MaxPlayers: 2, Game: hiddenGame{stubGame{key: "cards"}}, MaxPlies: 30}} {
		if err := games.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	players := []engine.PlayerID{"a", "b"}
	if _, err := games.NewState("cards", players); !errors.Is(err, engine.ErrSeedRequired) {
		t.Fatalf("a seeded game was dealt without a seed: %v", err)
	}
	if _, err := games.NewSeededState("cards", []engine.PlayerID{"a", "a"}, 1); !errors.Is(err, engine.ErrDuplicatePlayer) {
		t.Fatalf("seeded deal skipped the roster check: %v", err)
	}
	dealt, err := games.NewSeededState("cards", players, 1234)
	if err != nil || dealt.Seed != 1234 || dealt.MaxPlies != 30 || string(dealt.Data) != `{"deck":70,"hands":["a","b"]}` {
		t.Fatalf("seeded deal = %+v, %v", dealt, err)
	}
	plain, err := games.NewSeededState(testKey, players, 1234)
	if err != nil || plain.Seed != 0 {
		t.Fatalf("an open game kept a seed: %+v, %v", plain, err)
	}
	encoded, _ := json.Marshal(plain)
	if strings.Contains(string(encoded), "seed") {
		t.Fatalf("an unseeded state grew a seed field: %s", encoded)
	}
	round, _ := json.Marshal(dealt)
	var back engine.State
	if err := json.Unmarshal(round, &back); err != nil || back.Seed != 1234 {
		t.Fatalf("seed did not survive JSON: %s %v", round, err)
	}
	if !games.Hidden("cards") || games.Hidden(testKey) || games.Hidden("unknown") {
		t.Fatal("hidden information is reported by the optional Viewer")
	}
	view := games.View(dealt, "a")
	if view.Seed != 0 || string(view.Data) != `{"viewer":"a"}` {
		t.Fatalf("view = %+v", view)
	}
	if open := games.View(plain, "a"); string(open.Data) != string(plain.Data) {
		t.Fatal("an open game changed under View")
	}
}

func TestRegistryNewStateCarriesTheMoveLimit(t *testing.T) {
	registry := engine.NewRegistry()
	capped := stubDefinition(testKey, 2, 2)
	capped.MaxPlies = 40
	if err := registry.Register(capped); err != nil {
		t.Fatalf("Register(): %v", err)
	}
	if err := registry.Register(stubDefinition("open", 2, 2)); err != nil {
		t.Fatalf("Register(): %v", err)
	}

	state, err := registry.NewState(testKey, []engine.PlayerID{alice, bob})
	if err != nil {
		t.Fatalf("NewState(): %v", err)
	}
	if state.MaxPlies != 40 {
		t.Errorf("MaxPlies = %d, want 40", state.MaxPlies)
	}

	open, err := registry.NewState("open", []engine.PlayerID{alice, bob})
	if err != nil {
		t.Fatalf("NewState(): %v", err)
	}
	if open.MaxPlies != 0 {
		t.Errorf("MaxPlies without a limit = %d, want 0", open.MaxPlies)
	}
}

func TestRegistryRejectsANegativeMoveLimit(t *testing.T) {
	def := stubDefinition(testKey, 2, 2)
	def.MaxPlies = -1
	if err := engine.NewRegistry().Register(def); !errors.Is(err, engine.ErrInvalidDefinition) {
		t.Errorf("Register() error = %v, want %v", err, engine.ErrInvalidDefinition)
	}
}
