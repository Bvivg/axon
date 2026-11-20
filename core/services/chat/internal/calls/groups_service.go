package calls

import (
	"context"
	"time"

	"github.com/google/uuid"

	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
)

func (s *Service) FromCall(ctx context.Context, actor Actor, cmd Command) (Call, error) {
	if !s.SFUEnabled() {
		return Call{}, ErrUnavailable
	}
	if cmd.ID == uuid.Nil || cmd.TargetID == uuid.Nil || cmd.RequestID == uuid.Nil || !validRef(cmd.Endpoint) {
		return Call{}, ErrInvalid
	}
	if err := s.preflight(ctx, actor); err != nil {
		return Call{}, err
	}
	if err := s.media.Available(ctx); err != nil {
		return Call{}, err
	}
	store, ok := s.store.(GroupStore)
	if !ok {
		return Call{}, ErrUnavailable
	}
	cmd.Action = "replace"
	return store.ReplaceCall(ctx, actor, cmd, s.now().UTC())
}
func (s *Service) SfuCredentials(ctx context.Context, actor Actor, ref EndpointRef, id uuid.UUID, generation int64) (string, string, []*callv1.IceServer, time.Time, error) {
	if s.media == nil {
		return "", "", nil, time.Time{}, ErrUnavailable
	}
	if err := s.preflight(ctx, actor); err != nil {
		return "", "", nil, time.Time{}, err
	}
	call, err := s.selected(ctx, actor, ref, id, generation)
	if err != nil {
		return "", "", nil, time.Time{}, err
	}
	p := call.Participant(actor.UserID)
	if call.TransportKind() != "sfu" || call.TransportPhase == "waiting_cleanup" || !p.SupportsSFU || p.MediaVersion < 1 {
		return "", "", nil, time.Time{}, ErrConflict
	}
	servers, _, err := s.turnCredentials(ref)
	if err != nil {
		return "", "", nil, time.Time{}, err
	}
	token, expires, err := s.media.Issue(ctx, call, *p)
	if err != nil && !call.EverActive {
		_, _ = s.Execute(ctx, actor, Command{Action: "media_failed", ID: id, Endpoint: ref, RequestID: uuid.New()})
	}
	return s.media.PublicURL, token, servers, expires, err
}
