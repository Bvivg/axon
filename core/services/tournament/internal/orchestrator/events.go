package orchestrator

import (
	"encoding/binary"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

const (
	RoundStartedTopic = "tournament.round_started.v1"
	ForfeitTopic      = "tournament.forfeit.v1"
	CancelledTopic    = "tournament.cancelled.v1"
	FinishedTopic     = "tournament.finished.v1"
)

var eventNamespace = uuid.MustParse("c0f1e6a4-7b2d-4c58-9e3a-1d5f8b7a6c40")

func EventID(subject uuid.UUID, kind string, number int) uuid.UUID {
	name := make([]byte, 0, len(subject)+len(kind)+4)
	name = append(name, subject[:]...)
	name = append(name, kind...)
	name = binary.BigEndian.AppendUint32(name, uint32(number))
	return uuid.NewSHA1(eventNamespace, name)
}

func pairing(m saga.Match) *eventsv1.TournamentPairing {
	return &eventsv1.TournamentPairing{Board: uint32(m.Board), SessionId: m.Session.String(), Players: []string{m.Seats[0].String(), m.Seats[1].String()}}
}

func Announce(tournament uuid.UUID, effect saga.Effect) (outbox.Event, bool) {
	switch e := effect.(type) {
	case saga.RoundStarted:
		id := EventID(tournament, "round_started", e.Round)
		payload := &eventsv1.TournamentRoundStarted{EventId: id.String(), TournamentId: tournament.String(), Game: e.Game, Round: uint32(e.Round),
			StartedAt: timestamppb.New(e.StartedAt)}
		for _, m := range e.Matches {
			payload.Pairings = append(payload.Pairings, pairing(m))
		}
		return outbox.Event{ID: id, Topic: RoundStartedTopic, AggregateID: tournament.String(), Payload: payload}, true
	case saga.ForfeitIssued:
		id := EventID(e.Match.Session, "forfeit", 0)
		payload := &eventsv1.TournamentForfeit{EventId: id.String(), TournamentId: tournament.String(), Game: e.Game, Round: uint32(e.Match.Round),
			Pairing: pairing(e.Match), DueAt: timestamppb.New(e.Due)}
		return outbox.Event{ID: id, Topic: ForfeitTopic, AggregateID: tournament.String(), Payload: payload}, true
	case saga.Cancellation:
		id := EventID(tournament, "cancelled", 0)
		payload := &eventsv1.TournamentCancelled{EventId: id.String(), TournamentId: tournament.String(), Started: e.Started, CancelledAt: timestamppb.New(e.At)}
		for _, s := range e.Sessions {
			payload.SessionIds = append(payload.SessionIds, s.String())
		}
		for _, p := range e.Returned {
			payload.ReturnedPlayers = append(payload.ReturnedPlayers, p.String())
		}
		return outbox.Event{ID: id, Topic: CancelledTopic, AggregateID: tournament.String(), Payload: payload}, true
	case saga.Completion:
		id := EventID(tournament, "finished", 0)
		payload := &eventsv1.TournamentFinished{EventId: id.String(), TournamentId: tournament.String(), Game: e.Game, WinnerId: e.Winner.String(),
			FinishedAt: timestamppb.New(e.At)}
		for _, s := range e.Standings {
			payload.Standings = append(payload.Standings, &eventsv1.TournamentStanding{Rank: uint32(s.Rank), UserId: s.User.String(), HalfPoints: uint32(s.HalfPoints)})
		}
		return outbox.Event{ID: id, Topic: FinishedTopic, AggregateID: tournament.String(), Payload: payload}, true
	}
	return outbox.Event{}, false
}
