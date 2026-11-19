package identity

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/internalrpc"
	"github.com/bvivg/axon/core/shared/pkg/middleware"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func (r *Resolver) ActiveFamily(ctx context.Context, token string, familyID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	req := connect.NewRequest(&authv1.ListSessionsRequest{IncludePresence: proto.Bool(false)})
	req.Header().Set(authn.Header, "Bearer "+token)
	response, err := r.client.ListSessions(ctx, req)
	if err != nil {
		return fmt.Errorf("identity: active sessions: %w", err)
	}
	for _, session := range response.Msg.GetSessions() {
		if session.GetId() == familyID.String() {
			return nil
		}
	}
	return calls.ErrForbidden
}

type Resolver struct {
	media   authv1connect.SessionControlServiceClient
	client  authv1connect.AuthServiceClient
	timeout time.Duration
	log     *slog.Logger
}

type Config struct {
	HTTPClient connect.HTTPClient

	BaseURL string

	Timeout time.Duration

	Logger *slog.Logger
}

func New(cfg Config) (*Resolver, error) {
	switch {
	case cfg.HTTPClient == nil:
		return nil, errors.New("identity: http client is required")
	case cfg.BaseURL == "":
		return nil, errors.New("identity: auth service URL is required")
	case cfg.Logger == nil:
		return nil, errors.New("identity: logger is required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	return &Resolver{
		media: authv1connect.NewSessionControlServiceClient(cfg.HTTPClient, cfg.BaseURL),
		client: authv1connect.NewAuthServiceClient(cfg.HTTPClient, cfg.BaseURL,
			connect.WithInterceptors(middleware.NewCorrelationInterceptor()),
		),
		timeout: timeout,
		log:     cfg.Logger,
	}, nil
}

func (r *Resolver) CheckMediaFamily(ctx context.Context, userID, familyID uuid.UUID, key string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	req := connect.NewRequest(&authv1.CheckFamilyRequest{UserId: userID.String(), FamilyId: familyID.String()})
	req.Header().Set(internalrpc.Header, key)
	response, err := r.media.CheckFamily(ctx, req)
	if err != nil {
		return err
	}
	if !response.Msg.Active {
		return calls.ErrForbidden
	}
	return nil
}

func (r *Resolver) UpdateLastSeen(ctx context.Context, accessToken string) {
	if accessToken == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	req := connect.NewRequest(&authv1.UpdateLastSeenRequest{})
	req.Header().Set(authn.Header, "Bearer "+accessToken)

	if _, err := r.client.UpdateLastSeen(ctx, req); err != nil {
		r.log.WarnContext(ctx, "could not record last seen", "error", err)
	}
}

func (r *Resolver) DisplayName(ctx context.Context, accessToken string) string {
	if accessToken == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	req := connect.NewRequest(&authv1.GetMeRequest{})
	req.Header().Set(authn.Header, "Bearer "+accessToken)

	resp, err := r.client.GetMe(ctx, req)
	if err != nil {
		r.log.WarnContext(ctx, "could not read the caller's profile", "error", err)
		return ""
	}

	user := resp.Msg.GetUser()
	return cmp.Or(user.GetDisplayName(), user.GetUsername())
}

func (r *Resolver) Profiles(ctx context.Context, accessToken string, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	if len(ids) == 0 {
		return map[uuid.UUID]string{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	raw := make([]string, len(ids))
	for i, id := range ids {
		raw[i] = id.String()
	}

	req := connect.NewRequest(&authv1.GetUsersPublicProfilesRequest{UserIds: raw})
	req.Header().Set(authn.Header, "Bearer "+accessToken)

	resp, err := r.client.GetUsersPublicProfiles(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("identity: read profiles: %w", err)
	}

	names := make(map[uuid.UUID]string, len(ids))
	for _, user := range resp.Msg.GetUsers() {
		id, err := uuid.Parse(user.GetId())
		if err != nil {
			continue
		}
		names[id] = cmp.Or(user.GetDisplayName(), user.GetUsername())
	}
	for _, id := range ids {
		if _, ok := names[id]; !ok {
			return nil, domain.ErrUnknownUser
		}
	}

	return names, nil
}

func (r *Resolver) CheckCallPeer(ctx context.Context, accessToken string, peer uuid.UUID) error {
	_, err := r.Profiles(ctx, accessToken, []uuid.UUID{peer})
	if errors.Is(err, domain.ErrUnknownUser) {
		return calls.ErrNotFound
	}
	return err
}

func BearerFromHeader(h http.Header) string {
	raw, err := authn.BearerToken(h)
	if err != nil {
		return ""
	}
	return raw
}
