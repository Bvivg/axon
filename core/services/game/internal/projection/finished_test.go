package projection_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/projection"
	"github.com/bvivg/axon/core/services/game/internal/session"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

func TestFinishedEvent(t *testing.T) {
	creator, opponent := uuid.MustParse("1d6f2f43-9b35-4a6f-8f43-2a1c9d0e7b11"), uuid.MustParse("2e7a3b54-ac46-4b70-9054-3b2dae1f8c22")
	id := uuid.MustParse("0b8a1c56-2f3a-4a11-9b7e-4f0d7a1e9c01")
	at := time.Date(2026, 11, 20, 10, 30, 0, 0, time.FixedZone("UTC+3", 3*3600))
	state := func(status engine.Status, winner int) engine.State {
		return engine.State{Game: "chess", Status: status, Winner: winner, Turn: engine.NoSeat}
	}
	tests := []struct {
		name    string
		game    session.Session
		want    *eventsv1.GameFinished
		missing bool
	}{
		{
			name: "rated win by the opponent",
			game: session.Session{ID: id, Game: "chess", Creator: creator, Opponent: &opponent, Rated: true, State: state(engine.StatusWin, 1)},
			want: &eventsv1.GameFinished{Outcome: eventsv1.GameFinished_OUTCOME_WIN, WinnerSeat: 1, Rated: true},
		},
		{
			name: "unrated win by the creator",
			game: session.Session{ID: id, Game: "chess", Creator: creator, Opponent: &opponent, State: state(engine.StatusWin, 0)},
			want: &eventsv1.GameFinished{Outcome: eventsv1.GameFinished_OUTCOME_WIN, WinnerSeat: 0},
		},
		{
			name: "draw",
			game: session.Session{ID: id, Game: "chess", Creator: creator, Opponent: &opponent, Rated: true, AgreedDraw: true, State: state(engine.StatusDraw, engine.NoSeat)},
			want: &eventsv1.GameFinished{Outcome: eventsv1.GameFinished_OUTCOME_DRAW, WinnerSeat: -1, Rated: true},
		},
		{
			name:    "still in progress",
			game:    session.Session{ID: id, Game: "chess", Creator: creator, Opponent: &opponent, Rated: true, State: state(engine.StatusInProgress, engine.NoSeat)},
			missing: true,
		},
		{
			name:    "waiting for an opponent",
			game:    session.Session{ID: id, Game: "chess", Creator: creator},
			missing: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, ok := projection.FinishedEvent(tt.game, at)
			if ok == tt.missing {
				t.Fatalf("FinishedEvent() ok = %v, want %v", ok, !tt.missing)
			}
			if tt.missing {
				return
			}
			want := proto.Clone(tt.want).(*eventsv1.GameFinished)
			want.EventId, want.SessionId, want.Game = projection.FinishedEventID(id).String(), id.String(), "chess"
			want.Players, want.FinishedAt = []string{creator.String(), opponent.String()}, timestamppb.New(at.UTC())
			if event.ID != projection.FinishedEventID(id) || event.Topic != "game.finished.v1" || event.AggregateID != "chess" {
				t.Fatalf("envelope = %+v", event)
			}
			if !proto.Equal(event.Payload, want) {
				t.Fatalf("payload = %v, want %v", event.Payload, want)
			}
		})
	}
}

func TestFinishedEventIDIsStablePerSession(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	first, again := projection.FinishedEventID(a), projection.FinishedEventID(uuid.MustParse(a.String()))
	if first != again {
		t.Fatal("same session produced different event ids")
	}
	if projection.FinishedEventID(a) == projection.FinishedEventID(b) || projection.FinishedEventID(a) == a {
		t.Fatal("event id collides")
	}
}

func TestOutboxMigrationMatchesTheSharedSchema(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/000008_outbox.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != strings.TrimSpace(outbox.Migration("game")) {
		t.Fatalf("000008_outbox.up.sql drifted from outbox.Migration(\"game\"):\n%s", raw)
	}
}
