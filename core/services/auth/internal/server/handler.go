package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

type Handler struct {
	svc      *service.Service
	verifier *authn.Verifier
	log      *slog.Logger
	now      func() time.Time
}

var _ authv1connect.AuthServiceHandler = (*Handler)(nil)

type Config struct {
	Service  *service.Service
	Verifier *authn.Verifier
	Logger   *slog.Logger

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

	return &Handler{svc: cfg.Service, verifier: cfg.Verifier, log: cfg.Logger, now: now}, nil
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
	})
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.RegisterResponse{
		User:   toProtoUser(res.User),
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
	})
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.LoginResponse{
		User:   toProtoUser(res.User),
		Tokens: toProtoTokens(res.Tokens, h.now()),
	}), nil
}

func (h *Handler) RefreshToken(
	ctx context.Context,
	req *connect.Request[authv1.RefreshTokenRequest],
) (*connect.Response[authv1.RefreshTokenResponse], error) {
	res, err := h.svc.Refresh(ctx, req.Msg.GetRefreshToken())
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

	return connect.NewResponse(&authv1.GetMeResponse{User: toProtoUser(user)}), nil
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
	})
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&authv1.CompleteOAuthResponse{
		User:    toProtoUser(completed.User),
		Tokens:  toProtoTokens(completed.Tokens, h.now()),
		Created: completed.Created,
	}), nil
}

func (h *Handler) authenticate(headers http.Header) (authn.Claims, error) {
	raw, err := authn.BearerToken(headers)
	if err != nil {
		return authn.Claims{}, err
	}
	return h.verifier.Verify(raw)
}
