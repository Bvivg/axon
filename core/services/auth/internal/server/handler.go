package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/auth/internal/avatar"
	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

type Handler struct {
	svc        *service.Service
	verifier   *authn.Verifier
	log        *slog.Logger
	now        func() time.Time
	avatarURLs avatar.URLBuilder
}

var _ authv1connect.AuthServiceHandler = (*Handler)(nil)

type Config struct {
	Service  *service.Service
	Verifier *authn.Verifier
	Logger   *slog.Logger

	AvatarURLs avatar.URLBuilder

	Now func() time.Time
}

func New(cfg Config) (*Handler, error) {
	switch {
	case cfg.Service == nil:
		return nil, errors.New("server: service is required")
	case cfg.Verifier == nil:
		return nil, errors.New("server: token verifier is required")
	case cfg.Logger == nil:
		return nil, errors.New("server: logger is required")
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Handler{
		svc:        cfg.Service,
		verifier:   cfg.Verifier,
		log:        cfg.Logger,
		now:        now,
		avatarURLs: cfg.AvatarURLs,
	}, nil
}

func (h *Handler) Register(
	ctx context.Context,
	req *connect.Request[authv1.RegisterRequest],
) (*connect.Response[authv1.RegisterResponse], error) {
	msg := req.Msg

	res, err := h.svc.Register(ctx, service.RegisterInput{
		Email:       msg.GetEmail(),
		Password:    msg.GetPassword(),
		DisplayName: msg.GetDisplayName(),
		Device:      deviceFrom(req.Header()),
	})
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.RegisterResponse{
		User:   toProtoUser(res.User, h.avatarURLs),
		Tokens: toProtoTokens(res.Tokens, h.now()),
	}), nil
}

func (h *Handler) Login(
	ctx context.Context,
	req *connect.Request[authv1.LoginRequest],
) (*connect.Response[authv1.LoginResponse], error) {
	res, err := h.svc.Login(ctx, service.LoginInput{
		Email:    req.Msg.GetEmail(),
		Password: req.Msg.GetPassword(),
		Device:   deviceFrom(req.Header()),
	})
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.LoginResponse{
		User:   toProtoUser(res.User, h.avatarURLs),
		Tokens: toProtoTokens(res.Tokens, h.now()),
	}), nil
}

func (h *Handler) RefreshToken(
	ctx context.Context,
	req *connect.Request[authv1.RefreshTokenRequest],
) (*connect.Response[authv1.RefreshTokenResponse], error) {
	res, err := h.svc.Refresh(ctx, req.Msg.GetRefreshToken(), deviceFrom(req.Header()))
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.RefreshTokenResponse{
		Tokens: toProtoTokens(res.Tokens, h.now()),
	}), nil
}

func (h *Handler) Logout(
	ctx context.Context,
	req *connect.Request[authv1.LogoutRequest],
) (*connect.Response[authv1.LogoutResponse], error) {
	if err := h.svc.Logout(ctx, req.Msg.GetRefreshToken()); err != nil {
		return nil, translateError(ctx, h.log, err)
	}
	return connect.NewResponse(&authv1.LogoutResponse{}), nil
}

func (h *Handler) GetMe(
	ctx context.Context,
	req *connect.Request[authv1.GetMeRequest],
) (*connect.Response[authv1.GetMeResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	user, err := h.svc.Me(ctx, claims.UserID)
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.GetMeResponse{User: toProtoUser(user, h.avatarURLs)}), nil
}

func (h *Handler) StartOAuth(
	ctx context.Context,
	req *connect.Request[authv1.StartOAuthRequest],
) (*connect.Response[authv1.StartOAuthResponse], error) {
	provider, ok := fromProtoProvider(req.Msg.GetProvider())
	if !ok {
		return nil, translateError(ctx, h.log, domain.ErrProviderUnsupported)
	}

	started, err := h.svc.StartOAuth(ctx, provider, req.Msg.GetReturnTo())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.StartOAuthResponse{
		AuthorizationUrl: started.AuthorizationURL,
		State:            started.State,
	}), nil
}

func (h *Handler) CompleteOAuth(
	ctx context.Context,
	req *connect.Request[authv1.CompleteOAuthRequest],
) (*connect.Response[authv1.CompleteOAuthResponse], error) {
	provider, ok := fromProtoProvider(req.Msg.GetProvider())
	if !ok {
		return nil, translateError(ctx, h.log, domain.ErrProviderUnsupported)
	}

	completed, err := h.svc.CompleteOAuth(ctx, service.CompleteOAuthInput{
		Provider:    provider,
		Code:        req.Msg.GetCode(),
		State:       req.Msg.GetState(),
		DisplayName: req.Msg.GetDisplayName(),
		Device:      deviceFrom(req.Header()),
	})
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.CompleteOAuthResponse{
		User:    toProtoUser(completed.User, h.avatarURLs),
		Tokens:  toProtoTokens(completed.Tokens, h.now()),
		Created: completed.Created,
	}), nil
}

func (h *Handler) UpdateProfile(
	ctx context.Context,
	req *connect.Request[authv1.UpdateProfileRequest],
) (*connect.Response[authv1.UpdateProfileResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	user, err := h.svc.UpdateProfile(ctx, claims.UserID, domain.ProfilePatch{
		Email:     req.Msg.Email,
		FirstName: req.Msg.FirstName,
		LastName:  req.Msg.LastName,
		Nickname:  req.Msg.Nickname,
	})
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.UpdateProfileResponse{User: toProtoUser(user, h.avatarURLs)}), nil
}

func (h *Handler) ListSessions(
	ctx context.Context,
	req *connect.Request[authv1.ListSessionsRequest],
) (*connect.Response[authv1.ListSessionsResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	sessions, err := h.svc.ListSessions(ctx, claims.UserID, claims.FamilyID)
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	out := make([]*authv1.Session, len(sessions))
	for i, s := range sessions {
		out[i] = toProtoSession(s)
	}

	return connect.NewResponse(&authv1.ListSessionsResponse{Sessions: out}), nil
}

func (h *Handler) RevokeSession(
	ctx context.Context,
	req *connect.Request[authv1.RevokeSessionRequest],
) (*connect.Response[authv1.RevokeSessionResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	sessionID, err := uuid.Parse(req.Msg.GetSessionId())
	if err != nil {
		return nil, translateError(ctx, h.log, domain.ErrSessionNotFound)
	}

	if err := h.svc.RevokeSession(ctx, claims.UserID, sessionID); err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.RevokeSessionResponse{}), nil
}

func (h *Handler) GetUserByEmail(
	ctx context.Context,
	req *connect.Request[authv1.GetUserByEmailRequest],
) (*connect.Response[authv1.GetUserByEmailResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	user, err := h.svc.UserByEmail(ctx, claims.UserID, req.Msg.GetEmail())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.GetUserByEmailResponse{User: toProtoUser(user, h.avatarURLs)}), nil
}

func (h *Handler) SearchUsers(
	ctx context.Context,
	req *connect.Request[authv1.SearchUsersRequest],
) (*connect.Response[authv1.SearchUsersResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	users, err := h.svc.SearchUsers(ctx, claims.UserID, req.Msg.GetQuery())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	out := make([]*authv1.PublicProfile, len(users))
	for i, u := range users {
		out[i] = toProtoPublicProfile(u, h.avatarURLs)
	}

	return connect.NewResponse(&authv1.SearchUsersResponse{Users: out}), nil
}

func (h *Handler) GetUsersPublicProfiles(
	ctx context.Context,
	req *connect.Request[authv1.GetUsersPublicProfilesRequest],
) (*connect.Response[authv1.GetUsersPublicProfilesResponse], error) {
	if _, err := h.authenticate(req.Header()); err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	ids := make([]uuid.UUID, 0, len(req.Msg.GetUserIds()))
	for _, raw := range req.Msg.GetUserIds() {
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}

	users, err := h.svc.UsersPublicProfiles(ctx, ids)
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	out := make([]*authv1.PublicProfile, len(users))
	for i, u := range users {
		out[i] = toProtoPublicProfile(u, h.avatarURLs)
	}

	return connect.NewResponse(&authv1.GetUsersPublicProfilesResponse{Users: out}), nil
}

func (h *Handler) UpdateLastSeen(
	ctx context.Context,
	req *connect.Request[authv1.UpdateLastSeenRequest],
) (*connect.Response[authv1.UpdateLastSeenResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	if err := h.svc.UpdateLastSeen(ctx, claims.UserID); err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.UpdateLastSeenResponse{}), nil
}

func (h *Handler) ListRoles(
	ctx context.Context,
	req *connect.Request[authv1.ListRolesRequest],
) (*connect.Response[authv1.ListRolesResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	roles, err := h.svc.ListRoles(ctx, claims.UserID)
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	out := make([]*authv1.Role, len(roles))
	for i, r := range roles {
		out[i] = toProtoRole(r)
	}

	return connect.NewResponse(&authv1.ListRolesResponse{Roles: out}), nil
}

func (h *Handler) AssignRole(
	ctx context.Context,
	req *connect.Request[authv1.AssignRoleRequest],
) (*connect.Response[authv1.AssignRoleResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	userID, err := uuid.Parse(req.Msg.GetUserId())
	if err != nil {
		return nil, translateError(ctx, h.log, domain.ErrUserNotFound)
	}
	roleID, err := uuid.Parse(req.Msg.GetRoleId())
	if err != nil {
		return nil, translateError(ctx, h.log, domain.ErrRoleNotFound)
	}

	if err := h.svc.AssignRole(ctx, claims.UserID, userID, roleID); err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.AssignRoleResponse{}), nil
}

func (h *Handler) RevokeRole(
	ctx context.Context,
	req *connect.Request[authv1.RevokeRoleRequest],
) (*connect.Response[authv1.RevokeRoleResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	userID, err := uuid.Parse(req.Msg.GetUserId())
	if err != nil {
		return nil, translateError(ctx, h.log, domain.ErrUserNotFound)
	}
	roleID, err := uuid.Parse(req.Msg.GetRoleId())
	if err != nil {
		return nil, translateError(ctx, h.log, domain.ErrRoleNotFound)
	}

	if err := h.svc.RevokeRole(ctx, claims.UserID, userID, roleID); err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.RevokeRoleResponse{}), nil
}

func deviceFrom(headers http.Header) domain.Device {
	return domain.Device{
		UserAgent: headers.Get("User-Agent"),
		IP:        headers.Get(authn.ClientIPHeader),
	}
}

func (h *Handler) authenticate(headers http.Header) (authn.Claims, error) {
	raw, err := authn.BearerToken(headers)
	if err != nil {
		return authn.Claims{}, err
	}
	return h.verifier.Verify(raw)
}
