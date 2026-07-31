// Package server exposes the auth service over Connect.
//
// It is a translation layer and nothing else: requests become service inputs,
// domain errors become Connect codes, domain types become wire types. No rule
// about how authentication works lives here.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/jwt"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

// authorizationHeader is where the access token arrives.
const authorizationHeader = "Authorization"

// bearerPrefix is the scheme, matched case-insensitively because RFC 7235 says
// the scheme token is case-insensitive and clients differ.
const bearerPrefix = "bearer "

// Handler implements the generated AuthService interface.
type Handler struct {
	svc      *service.Service
	verifier *jwt.Verifier
	log      *slog.Logger
	now      func() time.Time
}

var _ authv1connect.AuthServiceHandler = (*Handler)(nil)

// Config configures a Handler.
type Config struct {
	Service  *service.Service
	Verifier *jwt.Verifier
	Logger   *slog.Logger

	// Now overrides the clock. Tests set it; production leaves it nil.
	Now func() time.Time
}

// New returns a Handler.
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

// Register creates an account and signs it in.
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

// Login exchanges credentials for tokens.
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

// RefreshToken rotates a refresh token and issues a new pair.
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

// Logout revokes the refresh chain the token belongs to.
func (h *Handler) Logout(
	ctx context.Context,
	req *connect.Request[authv1.LogoutRequest],
) (*connect.Response[authv1.LogoutResponse], error) {
	if err := h.svc.Logout(ctx, req.Msg.GetRefreshToken()); err != nil {
		return nil, translateError(ctx, h.log, err)
	}
	return connect.NewResponse(&authv1.LogoutResponse{}), nil
}

// GetMe returns the caller's profile.
//
// The subject comes from the token on the request, never from the body — the
// contract has no field for a user id precisely so this method cannot be asked
// for somebody else's account.
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

// StartOAuth begins an authorization code flow.
func (h *Handler) StartOAuth(
	ctx context.Context,
	req *connect.Request[authv1.StartOAuthRequest],
) (*connect.Response[authv1.StartOAuthResponse], error) {
	if _, ok := fromProtoProvider(req.Msg.GetProvider()); !ok {
		return nil, translateError(ctx, h.log, domain.ErrProviderUnsupported)
	}

	// The providers land in their own change; refusing explicitly is better than
	// a handler that looks wired up and returns an empty URL.
	return nil, connect.NewError(connect.CodeUnimplemented,
		errors.New("oauth sign-in is not available yet"))
}

// CompleteOAuth finishes an authorization code flow.
func (h *Handler) CompleteOAuth(
	ctx context.Context,
	req *connect.Request[authv1.CompleteOAuthRequest],
) (*connect.Response[authv1.CompleteOAuthResponse], error) {
	if _, ok := fromProtoProvider(req.Msg.GetProvider()); !ok {
		return nil, translateError(ctx, h.log, domain.ErrProviderUnsupported)
	}

	return nil, connect.NewError(connect.CodeUnimplemented,
		errors.New("oauth sign-in is not available yet"))
}

// authenticate verifies the bearer token on a request.
//
// The gateway already verified it, and this verifies it again. That is
// deliberate: rules/security.md puts resource-level authorization in the service
// that owns the resource, and a service that trusts a header because "the
// gateway must have checked" is one misrouted request away from trusting anyone.
// Verification is local against the key set, so the cost is a signature check.
func (h *Handler) authenticate(headers http.Header) (jwt.Claims, error) {
	raw := headers.Get(authorizationHeader)
	if raw == "" {
		return jwt.Claims{}, connect.NewError(connect.CodeUnauthenticated,
			errors.New("no access token"))
	}

	if len(raw) < len(bearerPrefix) || !strings.EqualFold(raw[:len(bearerPrefix)], bearerPrefix) {
		return jwt.Claims{}, connect.NewError(connect.CodeUnauthenticated,
			errors.New("authorization header is not a bearer token"))
	}

	return h.verifier.Verify(strings.TrimSpace(raw[len(bearerPrefix):]))
}
