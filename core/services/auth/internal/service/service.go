// Package service holds the auth business logic.
//
// It depends on a Store interface rather than on the repository, so its rules
// can be tested without a database. Nothing here knows about Connect, HTTP or
// protobuf: the server layer translates.
package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/jwt"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
	"github.com/bvivg/axon/core/services/auth/internal/password"
)

// Store is the persistence the service needs.
//
// The multi-table operations are single methods on purpose: whether creating a
// user with a password takes one write or two is the store's problem, and
// exposing transactions here would put that requirement in the layer that has no
// business knowing whether the store even has them.
type Store interface {
	CreateUserWithPassword(ctx context.Context, u domain.User, passwordHash string) (domain.User, error)
	CreateUserWithOauthAccount(ctx context.Context, u domain.User, a domain.OauthAccount) (domain.User, error)

	UserByEmail(ctx context.Context, email string) (domain.User, error)
	UserByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	UpdateUserProfile(ctx context.Context, id uuid.UUID, displayName, avatarURL string) (domain.User, error)

	SetCredential(ctx context.Context, userID uuid.UUID, passwordHash string) error
	CredentialByUserID(ctx context.Context, userID uuid.UUID) (domain.Credential, error)

	CreateRefreshToken(ctx context.Context, t domain.RefreshToken) error
	RefreshTokenByHash(ctx context.Context, hash string) (domain.RefreshToken, error)
	RotateRefreshToken(ctx context.Context, spentID uuid.UUID, next domain.RefreshToken, at time.Time) (bool, error)
	RevokeFamily(ctx context.Context, familyID uuid.UUID, at time.Time) (int64, error)
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) (int64, error)

	OauthAccountByProviderID(ctx context.Context, p domain.Provider, providerUserID string) (domain.OauthAccount, error)
	LinkOauthAccount(ctx context.Context, a domain.OauthAccount) error
}

// Config configures the service.
type Config struct {
	// RefreshTTL is how long a refresh token stays usable.
	RefreshTTL time.Duration

	// OAuth wires up provider sign-in. It is optional: a deployment with no
	// provider configured still serves the password flow, and the OAuth
	// procedures answer ErrProviderUnsupported rather than failing at startup.
	OAuth *OAuthConfig

	// Now overrides the clock. Tests set it; production leaves it nil.
	Now func() time.Time
}

// OAuthConfig is everything provider sign-in needs. All three are required
// together: a registry with nowhere to keep state cannot complete a flow.
type OAuthConfig struct {
	Providers *oauth.Registry
	States    *oauth.StateStore
	ReturnTo  *oauth.ReturnToPolicy
}

// oauthDeps is the validated form of OAuthConfig.
type oauthDeps struct {
	providers *oauth.Registry
	states    *oauth.StateStore
	returnTo  *oauth.ReturnToPolicy
}

// Service implements the auth use cases.
type Service struct {
	store  Store
	hasher *password.Hasher
	issuer *jwt.Issuer
	log    *slog.Logger

	refreshTTL time.Duration
	now        func() time.Time

	// oauth is nil when no provider sign-in is configured.
	oauth *oauthDeps

	// dummyHash is verified against when no account matches, so a sign-in
	// attempt costs the same whether or not the address exists. Without it, the
	// unknown-email path returns without hashing and is measurably faster, which
	// turns login into a user-enumeration oracle no matter how careful the error
	// messages are.
	dummyHash string
}

// New validates the dependencies and returns a Service.
func New(store Store, hasher *password.Hasher, issuer *jwt.Issuer, log *slog.Logger, cfg Config) (*Service, error) {
	switch {
	case store == nil:
		return nil, errors.New("service: store is required")
	case hasher == nil:
		return nil, errors.New("service: password hasher is required")
	case issuer == nil:
		return nil, errors.New("service: token issuer is required")
	case log == nil:
		return nil, errors.New("service: logger is required")
	case cfg.RefreshTTL <= 0:
		return nil, errors.New("service: refresh token lifetime must be positive")
	}

	deps, err := validateOAuth(cfg.OAuth)
	if err != nil {
		return nil, err
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	// Computed once at startup rather than per failed login: the point is to
	// spend the same time as a real verification, not to spend it twice.
	dummyHash, err := hasher.Hash("this password matches no account")
	if err != nil {
		return nil, err
	}

	return &Service{
		store:      store,
		hasher:     hasher,
		issuer:     issuer,
		log:        log,
		refreshTTL: cfg.RefreshTTL,
		now:        now,
		oauth:      deps,
		dummyHash:  dummyHash,
	}, nil
}

// validateOAuth turns the optional configuration into dependencies, refusing a
// half-filled one. Partial OAuth configuration is worse than none: it starts
// cleanly and then fails in the middle of somebody's sign-in.
func validateOAuth(cfg *OAuthConfig) (*oauthDeps, error) {
	if cfg == nil {
		return nil, nil
	}

	switch {
	case cfg.Providers == nil:
		return nil, errors.New("service: oauth provider registry is required")
	case cfg.States == nil:
		return nil, errors.New("service: oauth state store is required")
	case cfg.ReturnTo == nil:
		return nil, errors.New("service: oauth return_to policy is required")
	}

	return &oauthDeps{
		providers: cfg.Providers,
		states:    cfg.States,
		returnTo:  cfg.ReturnTo,
	}, nil
}
