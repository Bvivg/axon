package orchestrator_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/tournament/internal/orchestrator"
	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
)

func TestAnnounceUsesStableEventIDs(t *testing.T) {
	tournament, a, b := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	match := saga.Match{Round: 2, Board: 3, Session: uuid.New(), Seats: [2]uuid.UUID{a, b}}
	cases := []struct {
		effect saga.Effect
		topic  string
	}{
		{saga.RoundStarted{Round: 2, Game: "chess", Matches: []saga.Match{match}, StartedAt: now}, orchestrator.RoundStartedTopic},
		{saga.ForfeitIssued{Match: match, Game: "chess", Due: now}, orchestrator.ForfeitTopic},
		{saga.Cancellation{Started: true, Sessions: []uuid.UUID{match.Session}, At: now}, orchestrator.CancelledTopic},
		{saga.Completion{Winner: a, Game: "chess", Standings: []saga.Standing{{Rank: 1, User: a, HalfPoints: 4}}, At: now}, orchestrator.FinishedTopic},
	}
	seen := map[uuid.UUID]bool{}
	for _, c := range cases {
		first, ok := orchestrator.Announce(tournament, c.effect)
		again, _ := orchestrator.Announce(tournament, c.effect)
		if !ok || first.ID != again.ID || first.Topic != c.topic || first.AggregateID != tournament.String() || seen[first.ID] {
			t.Fatalf("%T announced %+v", c.effect, first)
		}
		seen[first.ID] = true
	}
	round, _ := orchestrator.Announce(tournament, cases[0].effect)
	payload := round.Payload.(*eventsv1.TournamentRoundStarted)
	if payload.GetRound() != 2 || len(payload.GetPairings()) != 1 || payload.GetPairings()[0].GetSessionId() != match.Session.String() || payload.GetPairings()[0].GetPlayers()[1] != b.String() {
		t.Fatalf("round_started = %v", payload)
	}
	next, _ := orchestrator.Announce(tournament, saga.RoundStarted{Round: 3, Game: "chess", StartedAt: now})
	if next.ID == round.ID {
		t.Fatal("two rounds share an event id")
	}
	if orchestrator.TournamentID(a, b) != orchestrator.TournamentID(a, b) || orchestrator.TournamentID(a, b) == orchestrator.TournamentID(b, a) {
		t.Fatal("tournament ids are not stable per creator and request")
	}
}
