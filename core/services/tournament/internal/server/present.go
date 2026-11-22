package server

import (
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	tournamentv1 "github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1"
	"github.com/bvivg/axon/core/shared/pkg/admincrud"
)

var presentedStatuses = map[saga.Status]tournamentv1.TournamentStatus{
	saga.Registration: tournamentv1.TournamentStatus_TOURNAMENT_STATUS_REGISTRATION,
	saga.Seeding:      tournamentv1.TournamentStatus_TOURNAMENT_STATUS_SEEDING,
	saga.Running:      tournamentv1.TournamentStatus_TOURNAMENT_STATUS_RUNNING,
	saga.Finished:     tournamentv1.TournamentStatus_TOURNAMENT_STATUS_FINISHED,
	saga.Cancelled:    tournamentv1.TournamentStatus_TOURNAMENT_STATUS_CANCELLED,
}

var presentedResults = map[saga.Result]tournamentv1.MatchResult{
	saga.Pending: tournamentv1.MatchResult_MATCH_RESULT_PENDING,
	saga.Win:     tournamentv1.MatchResult_MATCH_RESULT_WIN,
	saga.Draw:    tournamentv1.MatchResult_MATCH_RESULT_DRAW,
	saga.Bye:     tournamentv1.MatchResult_MATCH_RESULT_BYE,
	saga.Aborted: tournamentv1.MatchResult_MATCH_RESULT_ABORTED,
}

func stamp(at *time.Time) *timestamppb.Timestamp {
	if at == nil {
		return nil
	}
	return timestamppb.New(*at)
}

func Present(t saga.Tournament, noShow time.Duration) *tournamentv1.Tournament {
	out := &tournamentv1.Tournament{
		Id: t.ID.String(), Name: t.Spec.Name, Game: t.Spec.Game, Status: presentedStatuses[t.Status], Capacity: uint32(t.Spec.Capacity),
		TotalRounds: uint32(t.TotalRounds), CurrentRound: uint32(t.Round), CreatorId: t.Creator.String(),
		CreatedAt: timestamppb.New(t.CreatedAt), StartedAt: stamp(t.StartedAt), FinishedAt: stamp(t.FinishedAt), CancelledAt: stamp(t.CancelledAt),
		NoShowSeconds: uint32(noShow.Seconds()), Etag: admincrud.Etag(t.Version),
	}
	for format, value := range formats {
		if value == t.Spec.Format {
			out.Format = format
		}
	}
	if t.Winner != uuid.Nil {
		out.WinnerId = proto.String(t.Winner.String())
	}
	registered := t.Registered()
	out.PlayerCount = uint32(len(registered))
	for _, p := range t.Players {
		r := &tournamentv1.Registration{UserId: p.User.String(), Rating: uint32(max(0, p.Rating)), RegisteredAt: timestamppb.New(p.RegisteredAt), Returned: p.Returned}
		if p.Seed > 0 {
			r.Seed = proto.Uint32(uint32(p.Seed))
		}
		out.Registrations = append(out.Registrations, r)
	}
	for _, r := range t.Rounds {
		round := &tournamentv1.Round{Number: uint32(r.Number), Status: tournamentv1.RoundStatus_ROUND_STATUS_RUNNING, StartedAt: timestamppb.New(r.StartedAt),
			FinishedAt: stamp(r.FinishedAt)}
		if r.FinishedAt != nil {
			round.Status = tournamentv1.RoundStatus_ROUND_STATUS_FINISHED
		}
		for _, m := range t.RoundMatches(r.Number) {
			match := &tournamentv1.Match{Board: uint32(m.Board), Result: presentedResults[m.Result], Forfeit: m.Forfeit, FinishedAt: stamp(m.FinishedAt),
				PlayerIds: []string{m.Seats[0].String()}}
			if !m.Bye() {
				match.PlayerIds = append(match.PlayerIds, m.Seats[1].String())
				match.SessionId = proto.String(m.Session.String())
			}
			if m.Winner != uuid.Nil {
				match.WinnerId = proto.String(m.Winner.String())
			}
			round.Matches = append(round.Matches, match)
		}
		out.Rounds = append(out.Rounds, round)
	}
	for _, s := range t.Standings() {
		standing := &tournamentv1.Standing{Rank: uint32(s.Rank), UserId: s.User.String(), HalfPoints: uint32(s.HalfPoints), Wins: uint32(s.Wins),
			Draws: uint32(s.Draws), Losses: uint32(s.Losses), Byes: uint32(s.Byes), Eliminated: s.Eliminated}
		if s.Seed > 0 {
			standing.Seed = proto.Uint32(uint32(s.Seed))
		}
		out.Standings = append(out.Standings, standing)
	}
	return out
}
