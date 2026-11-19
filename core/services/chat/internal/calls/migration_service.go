package calls

import (
	"context"
	"time"

	"github.com/google/uuid"

	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
)

type MigrationStore interface {
	SetEndpointMigration(context.Context, Actor, EndpointRef, bool, time.Time) error
}

func (s *Service) MigrationEnabled() bool {
	return s.migration && !s.disableNewMigration && s.SFUEnabled()
}
func (s *Service) RegisterMigrationEndpoint(ctx context.Context, actor Actor, resume *EndpointRef, sfu, migration bool) (Endpoint, EndpointRef, error) {
	ep, ref, err := s.RegisterMediaEndpoint(ctx, actor, resume, sfu)
	if err != nil || !s.migration {
		return ep, ref, err
	}
	store, ok := s.store.(MigrationStore)
	if !ok {
		return Endpoint{}, EndpointRef{}, ErrUnavailable
	}
	if err = store.SetEndpointMigration(ctx, actor, ref, sfu && migration, s.now().UTC()); err != nil {
		return Endpoint{}, EndpointRef{}, err
	}
	ep.SupportsMigration = sfu && migration
	return ep, ref, nil
}
func (s *Service) MigrationCredentials(ctx context.Context, actor Actor, ref EndpointRef, id, operation uuid.UUID, generation int64) (string, string, []*callv1.IceServer, time.Time, error) {
	if !s.migration || s.media == nil {
		return "", "", nil, time.Time{}, ErrUnavailable
	}
	if err := s.preflight(ctx, actor); err != nil {
		return "", "", nil, time.Time{}, err
	}
	if err := s.Check(ctx, actor, ref, false); err != nil {
		return "", "", nil, time.Time{}, err
	}
	call, err := s.store.GetCall(ctx, actor.UserID, id)
	if err != nil {
		return "", "", nil, time.Time{}, err
	}
	if call.Migration == nil || call.Migration.ID != operation || call.Migration.TargetGeneration != generation || !s.now().Before(call.Migration.Deadline) {
		return "", "", nil, time.Time{}, ErrStale
	}
	candidate, p, err := call.MigrationCandidate(actor.UserID)
	if err != nil {
		return "", "", nil, time.Time{}, err
	}
	if p.EndpointID != ref.ID || p.Epoch != ref.Epoch {
		return "", "", nil, time.Time{}, ErrForbidden
	}
	servers, _, err := s.turnCredentials(ref)
	if err != nil {
		return "", "", nil, time.Time{}, err
	}
	token, expires, err := s.media.Issue(ctx, candidate, p)
	return s.media.PublicURL, token, servers, expires, err
}
