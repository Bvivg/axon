package journal

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

var ErrMalformed = errors.New("journal: malformed record")

func Encode(entry Entry) ([]byte, error) {
	record := &gamev1.JournalRecord{}
	switch {
	case entry.Claim != nil:
		record.Entry = &gamev1.JournalRecord_Claim{Claim: &gamev1.PartitionClaimed{
			Generation: entry.Claim.Generation, OwnerId: entry.Claim.Owner, At: timestamppb.New(entry.Claim.At),
		}}
	case entry.Event != nil:
		event, err := encodeEvent(*entry.Event)
		if err != nil {
			return nil, err
		}
		record.Entry = &gamev1.JournalRecord_Session{Session: event}
	default:
		return nil, fmt.Errorf("%w: empty entry", ErrMalformed)
	}
	return proto.Marshal(record)
}

func encodeEvent(event Event) (*gamev1.SessionRecorded, error) {
	game, change := event.Session, event.Change
	out := &gamev1.SessionRecorded{
		SessionId: game.ID.String(), Seq: game.Revision, Generation: event.Generation, At: timestamppb.New(change.At),
	}
	if change.Fact != session.FactMoved {
		snapshot, err := encodeSnapshot(game)
		if err != nil {
			return nil, err
		}
		out.Snapshot = snapshot
	}
	if change.Actor != uuid.Nil {
		out.ActorId = change.Actor.String()
	}
	switch change.Fact {
	case session.FactCreated:
		request := ""
		if change.Request != uuid.Nil {
			request = change.Request.String()
		}
		out.Fact = &gamev1.SessionRecorded_Created{Created: &gamev1.SessionCreated{RequestId: request}}
	case session.FactJoined:
		joined := &gamev1.PlayerJoined{}
		if seat, ok := game.SeatOf(change.Actor); ok && seat > 1 {
			joined.Seat = proto.Uint32(uint32(seat))
		}
		out.Fact = &gamev1.SessionRecorded_Joined{Joined: joined}
	case session.FactLeft:
		out.Fact = &gamev1.SessionRecorded_Left{Left: &gamev1.PlayerLeft{}}
	case session.FactMoved:
		if change.Move == nil {
			return nil, fmt.Errorf("%w: move without record", ErrMalformed)
		}
		out.Fact = &gamev1.SessionRecorded_Moved{Moved: &gamev1.MoveApplied{
			Ply: uint32(change.Move.Ply), DataJson: change.Move.Data, Outcome: encodeOutcome(game),
		}}
	case session.FactDrawOffered:
		out.Fact = &gamev1.SessionRecorded_DrawOffered{DrawOffered: &gamev1.DrawOffered{}}
	case session.FactDrawDeclined:
		out.Fact = &gamev1.SessionRecorded_DrawDeclined{DrawDeclined: &gamev1.DrawDeclined{}}
	case session.FactDrawAgreed:
		out.Fact = &gamev1.SessionRecorded_DrawAgreed{DrawAgreed: &gamev1.DrawAgreed{}}
	case session.FactResigned:
		out.Fact = &gamev1.SessionRecorded_Resigned{Resigned: &gamev1.PlayerResigned{}}
	case session.FactTimedOut:
		out.Fact = &gamev1.SessionRecorded_TimedOut{TimedOut: &gamev1.ClockExpired{}}
	case session.FactUndone:
		out.Fact = &gamev1.SessionRecorded_Undone{Undone: &gamev1.MoveUndone{Ply: uint32(change.UndonePly)}}
	case session.FactCancelled:
		out.Fact = &gamev1.SessionRecorded_Cancelled{Cancelled: &gamev1.SessionCancelled{}}
	case session.FactClockAdjusted:
		out.Fact = &gamev1.SessionRecorded_ClockAdjusted{ClockAdjusted: &gamev1.ClockAdjusted{}}
	default:
		return nil, fmt.Errorf("%w: unknown fact %d", ErrMalformed, change.Fact)
	}
	return out, nil
}

func encodeOutcome(game session.Session) *gamev1.MoveOutcome {
	out := &gamev1.MoveOutcome{Status: string(game.State.Status), Turn: int32(game.State.Turn), Winner: int32(game.State.Winner)}
	if game.Clock != nil {
		out.RemainingMs = game.Clock.RemainingMS[:]
	}
	return out
}

func OutcomeOf(game session.Session) *Outcome {
	out := &Outcome{Status: game.State.Status, Turn: game.State.Turn, Winner: game.State.Winner}
	if game.Clock != nil {
		out.Remaining = game.Clock.RemainingMS[:]
	}
	return out
}

func encodeSnapshot(game session.Session) (*gamev1.SessionSnapshot, error) {
	state, err := json.Marshal(game.State)
	if err != nil {
		return nil, err
	}
	clock, err := json.Marshal(game.Clock)
	if err != nil {
		return nil, err
	}
	out := &gamev1.SessionSnapshot{
		Game: string(game.Game), CreatorId: game.Creator.String(), StateJson: state, ClockJson: clock,
		TimedOut: game.TimedOut, Rated: game.Rated, Resigned: game.Resigned, AgreedDraw: game.AgreedDraw,
	}
	if game.Opponent != nil {
		opponent := game.Opponent.String()
		out.OpponentId = &opponent
	}
	if game.DrawOffer != nil {
		offer := game.DrawOffer.String()
		out.DrawOfferUserId = &offer
	}
	if game.Tournament != nil {
		tournament := game.Tournament.String()
		out.TournamentId = &tournament
	}
	if game.Seats > 0 {
		out.Seats = proto.Uint32(uint32(game.Seats))
	}
	for _, player := range game.Seated {
		out.PlayerIds = append(out.PlayerIds, player.String())
	}
	if len(game.Options) > 0 {
		out.OptionsJson = game.Options
	}
	for _, seat := range game.Left {
		out.LeftSeats = append(out.LeftSeats, uint32(seat))
	}
	return out, nil
}

func Decode(partition int32, offset int64, raw []byte) (Entry, error) {
	record := &gamev1.JournalRecord{}
	if err := proto.Unmarshal(raw, record); err != nil {
		return Entry{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	entry := Entry{Partition: partition, Offset: offset}
	switch value := record.Entry.(type) {
	case *gamev1.JournalRecord_Claim:
		entry.Claim = &Claim{Generation: value.Claim.Generation, Owner: value.Claim.OwnerId, At: value.Claim.At.AsTime()}
	case *gamev1.JournalRecord_Session:
		event, err := decodeEvent(value.Session)
		if err != nil {
			return Entry{}, err
		}
		entry.Event = &event
	default:
		return Entry{}, fmt.Errorf("%w: empty record", ErrMalformed)
	}
	return entry, nil
}

func decodeEvent(in *gamev1.SessionRecorded) (Event, error) {
	id, err := uuid.Parse(in.SessionId)
	if err != nil {
		return Event{}, fmt.Errorf("%w: session id: %w", ErrMalformed, err)
	}
	game := session.Session{ID: id}
	partial := in.Snapshot == nil
	if !partial {
		if game, err = decodeSnapshot(id, in.Snapshot); err != nil {
			return Event{}, err
		}
	}
	game.Revision = in.Seq
	game.UpdatedAt = in.At.AsTime()
	change := session.Change{At: in.At.AsTime()}
	var outcome *Outcome
	if in.ActorId != "" {
		if change.Actor, err = uuid.Parse(in.ActorId); err != nil {
			return Event{}, fmt.Errorf("%w: actor: %w", ErrMalformed, err)
		}
	}
	switch fact := in.Fact.(type) {
	case *gamev1.SessionRecorded_Created:
		change.Fact = session.FactCreated
		if fact.Created.RequestId != "" {
			if change.Request, err = uuid.Parse(fact.Created.RequestId); err != nil {
				return Event{}, fmt.Errorf("%w: request: %w", ErrMalformed, err)
			}
		}
	case *gamev1.SessionRecorded_Joined:
		change.Fact = session.FactJoined
	case *gamev1.SessionRecorded_Moved:
		change.Fact = session.FactMoved
		record := &session.MoveRecord{Ply: int(fact.Moved.Ply), Player: change.Actor, Data: fact.Moved.DataJson}
		if len(fact.Moved.StateBeforeJson) > 0 {
			if err := json.Unmarshal(fact.Moved.StateBeforeJson, &record.StateBefore); err != nil {
				return Event{}, fmt.Errorf("%w: state before: %w", ErrMalformed, err)
			}
		}
		if len(fact.Moved.ClockBeforeJson) > 0 {
			if err := json.Unmarshal(fact.Moved.ClockBeforeJson, &record.ClockBefore); err != nil {
				return Event{}, fmt.Errorf("%w: clock before: %w", ErrMalformed, err)
			}
		}
		change.Move = record
		if recorded := fact.Moved.Outcome; recorded != nil {
			outcome = &Outcome{Status: engine.Status(recorded.Status), Turn: int(recorded.Turn), Winner: int(recorded.Winner), Remaining: recorded.RemainingMs}
		}
	case *gamev1.SessionRecorded_DrawOffered:
		change.Fact = session.FactDrawOffered
	case *gamev1.SessionRecorded_DrawDeclined:
		change.Fact = session.FactDrawDeclined
	case *gamev1.SessionRecorded_DrawAgreed:
		change.Fact = session.FactDrawAgreed
	case *gamev1.SessionRecorded_Resigned:
		change.Fact = session.FactResigned
	case *gamev1.SessionRecorded_TimedOut:
		change.Fact = session.FactTimedOut
	case *gamev1.SessionRecorded_Undone:
		change.Fact, change.UndonePly = session.FactUndone, int(fact.Undone.Ply)
	case *gamev1.SessionRecorded_Cancelled:
		change.Fact = session.FactCancelled
	case *gamev1.SessionRecorded_ClockAdjusted:
		change.Fact = session.FactClockAdjusted
	case *gamev1.SessionRecorded_Left:
		change.Fact = session.FactLeft
	default:
		return Event{}, fmt.Errorf("%w: unknown fact", ErrMalformed)
	}
	if partial && change.Fact != session.FactMoved {
		return Event{}, fmt.Errorf("%w: missing snapshot", ErrMalformed)
	}
	return Event{Session: game, Change: change, Generation: in.Generation, Partial: partial, Outcome: outcome}, nil
}

func decodeSnapshot(id uuid.UUID, in *gamev1.SessionSnapshot) (session.Session, error) {
	if in == nil {
		return session.Session{}, fmt.Errorf("%w: missing snapshot", ErrMalformed)
	}
	creator, err := uuid.Parse(in.CreatorId)
	if err != nil {
		return session.Session{}, fmt.Errorf("%w: creator: %w", ErrMalformed, err)
	}
	game := session.Session{
		ID: id, Game: engine.Key(in.Game), Creator: creator,
		TimedOut: in.TimedOut, Rated: in.Rated, Resigned: in.Resigned, AgreedDraw: in.AgreedDraw,
	}
	if in.OpponentId != nil {
		opponent, err := uuid.Parse(*in.OpponentId)
		if err != nil {
			return session.Session{}, fmt.Errorf("%w: opponent: %w", ErrMalformed, err)
		}
		game.Opponent = &opponent
	}
	if in.DrawOfferUserId != nil {
		offer, err := uuid.Parse(*in.DrawOfferUserId)
		if err != nil {
			return session.Session{}, fmt.Errorf("%w: draw offer: %w", ErrMalformed, err)
		}
		game.DrawOffer = &offer
	}
	if in.TournamentId != nil {
		tournament, err := uuid.Parse(*in.TournamentId)
		if err != nil {
			return session.Session{}, fmt.Errorf("%w: tournament: %w", ErrMalformed, err)
		}
		game.Tournament = &tournament
	}
	game.Seats = int(in.GetSeats())
	for _, raw := range in.PlayerIds {
		player, err := uuid.Parse(raw)
		if err != nil {
			return session.Session{}, fmt.Errorf("%w: player: %w", ErrMalformed, err)
		}
		game.Seated = append(game.Seated, player)
	}
	if len(in.OptionsJson) > 0 {
		game.Options = in.OptionsJson
	}
	for _, seat := range in.LeftSeats {
		game.Left = append(game.Left, int(seat))
	}
	if err := json.Unmarshal(in.StateJson, &game.State); err != nil {
		return session.Session{}, fmt.Errorf("%w: state: %w", ErrMalformed, err)
	}
	if err := json.Unmarshal(in.ClockJson, &game.Clock); err != nil {
		return session.Session{}, fmt.Errorf("%w: clock: %w", ErrMalformed, err)
	}
	return game, nil
}
