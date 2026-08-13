package engine_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

func TestNewState(t *testing.T) {
	roster := []engine.PlayerID{alice, bob}
	state := engine.NewState(testKey, roster, json.RawMessage(`{"cells":[]}`))

	if state.Game != testKey {
		t.Errorf("Game = %q, want %q", state.Game, testKey)
	}
	if state.Turn != 0 || state.Ply != 0 {
		t.Errorf("opening position at turn %d ply %d, want 0 and 0", state.Turn, state.Ply)
	}
	if state.Status != engine.StatusInProgress {
		t.Errorf("Status = %q, want %q", state.Status, engine.StatusInProgress)
	}
	if state.Winner != engine.NoSeat {
		t.Errorf("Winner = %d, want %d", state.Winner, engine.NoSeat)
	}

	roster[0] = carol
	if state.Players[0] != alice {
		t.Errorf("seat 0 followed the caller's slice to %q", state.Players[0])
	}
}

func TestAdvanceAndFinishLeaveTheReceiverAlone(t *testing.T) {
	state := newTestState()
	before, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}

	next := state.Advance(json.RawMessage(`{"moves":1}`), 1)
	done := next.Finish(1)

	after, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("the original position changed:\n before %s\n after  %s", before, after)
	}

	if next.Ply != state.Ply+1 {
		t.Errorf("Advance() ply = %d, want %d", next.Ply, state.Ply+1)
	}
	if next.Turn != 1 {
		t.Errorf("Advance() turn = %d, want 1", next.Turn)
	}
	if next.Over() {
		t.Error("Advance() ended the game")
	}

	if done.Status != engine.StatusWin || done.Winner != 1 {
		t.Errorf("Finish(1) = status %q winner %d, want %q and 1", done.Status, done.Winner, engine.StatusWin)
	}

	if done.Turn != engine.NoSeat {
		t.Errorf("Finish() left turn = %d, want %d", done.Turn, engine.NoSeat)
	}
	if next.Over() {
		t.Error("Finish() ended the position it was called on")
	}
}

func TestStateOutcome(t *testing.T) {
	base := newTestState()

	tests := []struct {
		name         string
		state        engine.State
		wantOver     bool
		wantWinner   engine.PlayerID
		wantNoWinner bool
	}{
		{
			name:         "still playing",
			state:        base,
			wantNoWinner: true,
		},
		{
			name:       "won by the first seat",
			state:      base.Finish(0),
			wantOver:   true,
			wantWinner: alice,
		},
		{
			name:       "won by the second seat",
			state:      base.Finish(1),
			wantOver:   true,
			wantWinner: bob,
		},
		{
			name:         "drawn",
			state:        base.Finish(engine.NoSeat),
			wantOver:     true,
			wantNoWinner: true,
		},
		{
			name: "finished naming a seat that does not exist",
			state: func() engine.State {
				s := base.Finish(0)
				s.Winner = 9
				return s
			}(),
			wantOver:     true,
			wantNoWinner: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			over, winner := tt.state.Outcome()

			if over != tt.wantOver {
				t.Errorf("Outcome() over = %v, want %v", over, tt.wantOver)
			}
			if tt.wantNoWinner {
				if winner != nil {
					t.Errorf("Outcome() winner = %q, want none", *winner)
				}
				return
			}
			if winner == nil {
				t.Fatalf("Outcome() winner = none, want %q", tt.wantWinner)
			}
			if *winner != tt.wantWinner {
				t.Errorf("Outcome() winner = %q, want %q", *winner, tt.wantWinner)
			}

			*winner = carol
			if _, again := tt.state.Outcome(); *again == carol {
				t.Error("the winner pointer wrote through into the position")
			}
		})
	}
}

func TestStateSeatAndCurrent(t *testing.T) {
	state := newTestState()

	if seat, ok := state.Seat(bob); !ok || seat != 1 {
		t.Errorf("Seat(bob) = %d, %v; want 1, true", seat, ok)
	}
	if seat, ok := state.Seat(carol); ok || seat != engine.NoSeat {
		t.Errorf("Seat(carol) = %d, %v; want %d, false", seat, ok, engine.NoSeat)
	}

	if player, ok := state.Current(); !ok || player != alice {
		t.Errorf("Current() = %q, %v; want alice, true", player, ok)
	}
	if _, ok := state.Finish(0).Current(); ok {
		t.Error("a finished position still named a player to move")
	}

	empty := engine.NewState(testKey, nil, json.RawMessage(`{}`))
	if _, ok := empty.Current(); ok {
		t.Error("a position with no players named a player to move")
	}
}

func TestStateAndMoveSurviveJSON(t *testing.T) {
	state := newTestState().
		Advance(json.RawMessage(`{"cells":[0,-1,1]}`), 1).
		Finish(1)

	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}

	var decoded engine.State
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}

	again, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal decoded state: %v", err)
	}
	if !bytes.Equal(encoded, again) {
		t.Errorf("state changed across a round trip:\n before %s\n after  %s", encoded, again)
	}

	move := engine.Move{Player: bob, Ply: 1, Data: json.RawMessage(`{"row":2,"col":0}`)}
	encodedMove, err := json.Marshal(move)
	if err != nil {
		t.Fatalf("marshal move: %v", err)
	}

	var decodedMove engine.Move
	if err := json.Unmarshal(encodedMove, &decodedMove); err != nil {
		t.Fatalf("unmarshal move: %v", err)
	}
	if decodedMove.Player != move.Player || decodedMove.Ply != move.Ply {
		t.Errorf("move changed across a round trip: %+v, want %+v", decodedMove, move)
	}
	if !bytes.Equal(decodedMove.Data, move.Data) {
		t.Errorf("move payload = %s, want %s", decodedMove.Data, move.Data)
	}
}
