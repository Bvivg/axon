package ws

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/services/chat/internal/pubsub"
	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

func isCallFrame(kind string) bool { return strings.HasPrefix(kind, "call_") }
func (s *session) callActor() calls.Actor {
	return calls.Actor{UserID: s.user, FamilyID: s.familyID, Token: s.token}
}
func (s *session) callEndpoint() *calls.EndpointRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.callRef == nil {
		return nil
	}
	value := *s.callRef
	return &value
}
func (s *session) writeCall(ctx context.Context, event *callv1.CallSocketEvent) bool {
	raw, err := protojson.Marshal(event)
	if err != nil {
		return false
	}
	if err := s.write(ctx, Outbound{Type: "call_event", Call: raw}); err != nil {
		_ = s.conn.CloseNow()
		return false
	}
	return true
}
func (s *session) callFailure(ctx context.Context, err error) bool {
	code := "unavailable"
	for _, item := range []struct {
		err  error
		code string
	}{{calls.ErrInvalid, "invalid"}, {calls.ErrForbidden, "forbidden"}, {calls.ErrStale, "stale"}, {calls.ErrConflict, "conflict"}, {calls.ErrBusy, "rate_limited"}} {
		if errors.Is(err, item.err) {
			code = item.code
			break
		}
	}
	message := "Call operation could not be completed"
	return s.writeCall(ctx, &callv1.CallSocketEvent{ErrorCode: &code, ErrorMessage: &message})
}
func (s *session) handleCall(ctx context.Context, frame Inbound) bool {
	if s.h.calls == nil {
		return s.callFailure(ctx, calls.ErrUnavailable)
	}
	if len(frame.Call) > 32768 {
		return s.callFailure(ctx, calls.ErrInvalid)
	}
	var wire callv1.CallSocketFrame
	if err := protojson.Unmarshal(frame.Call, &wire); err != nil {
		return s.callFailure(ctx, calls.ErrInvalid)
	}
	actor := s.callActor()
	if frame.Type == "call_attach" {
		ref := calls.RefFromProto(wire.Endpoint)
		if s.callConnection == uuid.Nil {
			s.callConnection = uuid.New()
		}
		if err := s.h.calls.Bind(ctx, actor, ref, s.callConnection); err != nil {
			return s.callFailure(ctx, err)
		}
		active, events, err := s.h.calls.Attach(ctx, actor, ref)
		if err != nil {
			return s.callFailure(ctx, err)
		}
		s.mu.Lock()
		s.callRef = &ref
		s.mu.Unlock()
		snapshot := &callv1.CallSocketEvent{Snapshot: true, EndpointEpoch: ref.Epoch}
		if active != nil {
			snapshot.Call = calls.ToProtoFor(*active, actor.UserID)
		}
		if !s.writeCall(ctx, snapshot) {
			return false
		}
		for _, event := range events {
			if !s.writeCall(ctx, calls.EventProto(event, ref.Epoch)) {
				return false
			}
		}
		return s.writeCall(ctx, &callv1.CallSocketEvent{ResyncComplete: true, HasMore: len(events) == 100, EndpointEpoch: ref.Epoch})
	}
	ref := s.callEndpoint()
	if ref == nil {
		return s.callFailure(ctx, calls.ErrForbidden)
	}
	switch frame.Type {
	case "call_heartbeat":
		if err := s.h.calls.Check(ctx, actor, *ref, true); err != nil {
			_ = s.conn.Close(ws.StatusGoingAway, "call lease unavailable")
			return false
		}
		return s.writeCall(ctx, &callv1.CallSocketEvent{EndpointEpoch: ref.Epoch})
	case "call_resync":
		active, events, err := s.h.calls.Attach(ctx, actor, *ref)
		if err != nil {
			return s.callFailure(ctx, err)
		}
		snapshot := &callv1.CallSocketEvent{Snapshot: true, EndpointEpoch: ref.Epoch}
		if active != nil {
			snapshot.Call = calls.ToProtoFor(*active, actor.UserID)
		}
		if !s.writeCall(ctx, snapshot) {
			return false
		}
		for _, event := range events {
			if !s.writeCall(ctx, calls.EventProto(event, ref.Epoch)) {
				return false
			}
		}
		return s.writeCall(ctx, &callv1.CallSocketEvent{ResyncComplete: true, HasMore: len(events) == 100, EndpointEpoch: ref.Epoch})
	case "call_ack":
		id, err := uuid.Parse(wire.GetEventId())
		if err != nil {
			return s.callFailure(ctx, calls.ErrInvalid)
		}
		if err := s.h.calls.Acknowledge(ctx, actor, *ref, id); err != nil {
			return s.callFailure(ctx, err)
		}
		return true
	case "call_signal":
		now := s.h.now()
		if now.Sub(s.callWindow) >= time.Second {
			s.callWindow = now
			s.callSignals = 0
		}
		s.callSignals++
		if s.callSignals > 64 {
			return s.callFailure(ctx, calls.ErrBusy)
		}
		if err := s.h.calls.SendSignal(ctx, actor, *ref, wire.Signal); err != nil {
			return s.callFailure(ctx, err)
		}
		return true
	default:
		return s.callFailure(ctx, calls.ErrInvalid)
	}
}
func (s *session) deliverCall(ctx context.Context, sig pubsub.Signal) {
	ref := s.callEndpoint()
	if ref == nil || s.h.calls == nil {
		return
	}
	if sig.Kind == pubsub.SignalCallState {
		event, err := s.h.calls.Event(ctx, s.callActor(), *ref, sig.EventID)
		if err != nil {
			return
		}
		s.writeCall(ctx, calls.EventProto(event, ref.Epoch))
		return
	}
	if sig.EndpointID != ref.ID {
		return
	}
	signal, err := s.h.calls.ReceiveSignal(ctx, s.callActor(), *ref, []byte(sig.Payload))
	if err != nil {
		return
	}
	s.writeCall(ctx, &callv1.CallSocketEvent{Signal: signal, EndpointEpoch: ref.Epoch})
}
