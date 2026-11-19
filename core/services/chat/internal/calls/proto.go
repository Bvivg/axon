package calls

import (
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
)

func ToProto(c Call) *callv1.Call {
	return ToProtoFor(c, uuid.Nil)
}
func ToProtoFor(c Call, viewer uuid.UUID) *callv1.Call {
	mode := callv1.CallMode_CALL_MODE_AUDIO
	if c.Mode == "video" {
		mode = callv1.CallMode_CALL_MODE_VIDEO
	}
	state := map[string]callv1.CallState{Ringing: callv1.CallState_CALL_STATE_RINGING, Connecting: callv1.CallState_CALL_STATE_CONNECTING, Active: callv1.CallState_CALL_STATE_ACTIVE, Ended: callv1.CallState_CALL_STATE_ENDED}[c.State]
	result := &callv1.Call{Id: c.ID.String(), RoomId: c.RoomID.String(), CallerId: c.CallerID.String(), Mode: mode, State: state, Version: c.Version, Generation: c.Generation, NegotiationId: c.NegotiationID.String(), EverActive: c.EverActive, CreatedAt: timestamppb.New(c.CreatedAt), EndReason: c.EndReason}
	if c.RoomID == uuid.Nil {
		result.RoomId = ""
	}
	if c.Group() || c.Transport != "" || c.FallbackAvailable {
		kind := "direct"
		if c.Group() {
			kind = "group"
		}
		transport := c.TransportKind()
		result.Kind = &kind
		result.Transport = &transport
		if c.TransportPhase != "" {
			phase := c.TransportPhase
			result.TransportPhase = &phase
		}
		if c.CleanupGeneration > 0 {
			generation := c.CleanupGeneration
			result.CleanupGeneration = &generation
		}
		fallback := c.FallbackAvailable
		result.SfuFallbackAvailable = &fallback
	}
	if c.OwnerID != uuid.Nil {
		owner := c.OwnerID.String()
		result.OwnerId = &owner
	}
	if m := c.Migration; m != nil {
		item := &callv1.CallMigration{Id: m.ID.String(), SourceGeneration: m.SourceGeneration, TargetGeneration: m.TargetGeneration, Phase: m.Phase, Outcome: m.Outcome}
		if !m.Deadline.IsZero() {
			item.Deadline = timestamppb.New(m.Deadline)
		}
		for _, p := range m.Peers {
			item.Participants = append(item.Participants, &callv1.MigrationPeer{UserId: p.UserID.String(), EndpointId: p.EndpointID.String(), EndpointEpoch: p.Epoch, MediaVersion: p.MediaVersion, Ready: p.Ready, Switched: p.Switched, CleanupDone: p.Clean, AbortCleanupDone: p.AbortClean})
		}
		result.Migration = item
	}
	if c.ReplacedByID != uuid.Nil {
		id := c.ReplacedByID.String()
		result.ReplacedByCallId = &id
	}
	for _, allowed := range c.PredecessorViewers {
		if allowed == viewer && c.PredecessorID != uuid.Nil {
			id := c.PredecessorID.String()
			result.PredecessorCallId = &id
		}
	}
	if c.StartedAt != nil {
		result.StartedAt = timestamppb.New(*c.StartedAt)
	}
	if c.EndedAt != nil {
		result.EndedAt = timestamppb.New(*c.EndedAt)
	}
	if !c.Deadline.IsZero() {
		result.Deadline = timestamppb.New(c.Deadline)
	}
	for _, p := range c.Participants {
		item := &callv1.Participant{UserId: p.UserID.String(), Status: p.Status, EndpointEpoch: p.Epoch, MediaReady: p.Ready, CleanupDone: p.CleanupDone, CleanupExpired: p.CleanupExpired}
		if p.SupportsMigration {
			value := true
			item.SupportsMigration = &value
		}
		if p.SupportsSFU {
			value := true
			item.SupportsSfu = &value
		}
		if p.MediaVersion > 0 {
			value := p.MediaVersion
			item.MediaVersion = &value
		}
		if !p.InviteUntil.IsZero() {
			item.InvitationDeadline = timestamppb.New(p.InviteUntil)
		}
		if p.EndpointID != uuid.Nil {
			id := p.EndpointID.String()
			item.EndpointId = &id
		}
		result.Participants = append(result.Participants, item)
	}
	if own := c.Participant(viewer); c.Group() && own != nil && own.Retired() {
		result.State = callv1.CallState_CALL_STATE_ENDED
		result.EndReason = own.Status
		result.OwnerId = nil
		if own.ConnectedAt == nil {
			result.StartedAt = nil
			result.EverActive = false
		}
		if own.LeftAt != nil {
			result.EndedAt = timestamppb.New(*own.LeftAt)
		}
		if own.ConnectedAt != nil && c.StartedAt != nil && own.ConnectedAt.After(*c.StartedAt) {
			result.StartedAt = timestamppb.New(*own.ConnectedAt)
		}
		for _, item := range result.Participants {
			if item.UserId == viewer.String() {
				result.Participants = []*callv1.Participant{item}
				break
			}
		}
	}
	if own := c.Participant(viewer); c.Group() && own != nil && own.Status == "invited" && c.State != Ended {
		result.StartedAt = nil
		result.EverActive = false
		if c.TransportPhase == "waiting_cleanup" {
			result.State = callv1.CallState_CALL_STATE_CONNECTING
		} else {
			result.State = callv1.CallState_CALL_STATE_RINGING
			result.Deadline = timestamppb.New(own.InviteUntil)
		}
	}
	if own := c.Participant(viewer); c.Group() && own != nil && own.Status == "accepted" && !own.Ready && c.State == Active {
		result.State = callv1.CallState_CALL_STATE_CONNECTING
		if !own.MediaDeadline.IsZero() {
			result.Deadline = timestamppb.New(own.MediaDeadline)
		}
		if own.ConnectedAt == nil {
			result.StartedAt = nil
			result.EverActive = false
		}
	}
	return result
}
func RefFromProto(ref *callv1.EndpointRef) EndpointRef {
	if ref == nil {
		return EndpointRef{}
	}
	id, _ := uuid.Parse(ref.Id)
	return EndpointRef{ID: id, Epoch: ref.Epoch, Proof: ref.Proof}
}
func EventProto(event Event, epoch int64) *callv1.CallSocketEvent {
	id := event.ID.String()
	return &callv1.CallSocketEvent{EventId: &id, Sequence: event.Sequence, Call: ToProtoFor(event.Call, event.UserID), EndpointEpoch: epoch}
}
func LeaseProto(expiry time.Time) *timestamppb.Timestamp { return timestamppb.New(expiry) }
