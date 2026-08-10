package engine_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

const (
	testKey engine.Key = "stub"

	alice engine.PlayerID = "alice"
	bob   engine.PlayerID = "bob"
	carol engine.PlayerID = "carol"
)

// newTestState is a position in a game nobody implements: Precheck only reads
// the envelope, so a payload that decodes into nothing is enough.
func newTestState() engine.State {
	return engine.NewState(testKey, []engine.PlayerID{alice, bob}, json.RawMessage(`{}`))
}

func TestPrecheck(t *testing.T) {
	fresh := newTestState()

	// The same position one move in, with the turn passed to the second seat.
	passed := fresh.Advance(json.RawMessage(`{}`), 1)

	otherGame := fresh
	otherGame.Game = "chess"

	nobodyToMove := fresh
	nobodyToMove.Turn = 7

	tests := []struct {
		name     string
		state    engine.State
		move     engine.Move
		wantSeat int
		wantErr  error
	}{
		{
			name:     "player whose turn it is",
			state:    fresh,
			move:     engine.Move{Player: alice, Ply: 0},
			wantSeat: 0,
		},
		{
			name:     "second seat once the turn has passed",
			state:    passed,
			move:     engine.Move{Player: bob, Ply: 1},
			wantSeat: 1,
		},
		{
			name:    "position belongs to another game",
			state:   otherGame,
			move:    engine.Move{Player: alice, Ply: 0},
			wantErr: engine.ErrGameMismatch,
		},
		{
			name:    "game is already decided",
			state:   fresh.Finish(0),
			move:    engine.Move{Player: alice, Ply: 0},
			wantErr: engine.ErrGameOver,
		},
		{
			name:    "mover holds no seat",
			state:   fresh,
			move:    engine.Move{Player: carol, Ply: 0},
			wantErr: engine.ErrPlayerNotInGame,
		},
		{
			name:    "mover is at the table but out of turn",
			state:   fresh,
			move:    engine.Move{Player: bob, Ply: 0},
			wantErr: engine.ErrOutOfTurn,
		},
		{
			// The case a turn check cannot catch: the right player sending the
			// same position's move twice.
			name:    "move computed against an older position",
			state:   passed,
			move:    engine.Move{Player: bob, Ply: 0},
			wantErr: engine.ErrStaleMove,
		},
		{
			name:    "turn names a seat nobody occupies",
			state:   nobodyToMove,
			move:    engine.Move{Player: alice, Ply: 0},
			wantErr: engine.ErrOutOfTurn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seat, err := engine.Precheck(testKey, tt.state, tt.move)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Precheck() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if seat != tt.wantSeat {
				t.Errorf("Precheck() seat = %d, want %d", seat, tt.wantSeat)
			}
		})
	}
}

func TestDecodeState(t *testing.T) {
	type payload struct {
		Moves int `json:"moves"`
	}

	tests := []struct {
		name    string
		data    json.RawMessage
		want    int
		wantErr error
	}{
		{
			name: "readable payload",
			data: json.RawMessage(`{"moves":4}`),
			want: 4,
		},
		{
			name:    "truncated payload",
			data:    json.RawMessage(`{"moves":`),
			wantErr: engine.ErrStateMalformed,
		},
		{
			name:    "no payload at all",
			wantErr: engine.ErrStateMalformed,
		},
		{
			name:    "payload of the wrong shape",
			data:    json.RawMessage(`["moves"]`),
			wantErr: engine.ErrStateMalformed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := engine.NewState(testKey, []engine.PlayerID{alice, bob}, tt.data)

			var got payload
			err := engine.DecodeState(state, &got)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("DecodeState() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && got.Moves != tt.want {
				t.Errorf("DecodeState() moves = %d, want %d", got.Moves, tt.want)
			}
		})
	}
}

func TestDecodeMove(t *testing.T) {
	type payload struct {
		Row int `json:"row"`
	}

	var got payload
	if err := engine.DecodeMove(engine.Move{Data: json.RawMessage(`{"row":2}`)}, &got); err != nil {
		t.Fatalf("DecodeMove() on a readable payload: %v", err)
	}
	if got.Row != 2 {
		t.Errorf("DecodeMove() row = %d, want 2", got.Row)
	}

	// The separate sentinel earns its keep here: a client sending nonsense and a
	// corrupt saved game are different answers to different people.
	err := engine.DecodeMove(engine.Move{Data: json.RawMessage(`"e2e4"`)}, &got)
	if !errors.Is(err, engine.ErrMoveMalformed) {
		t.Errorf("DecodeMove() error = %v, want %v", err, engine.ErrMoveMalformed)
	}
	if errors.Is(err, engine.ErrStateMalformed) {
		t.Error("a bad move payload was reported as a bad state payload")
	}
}
