package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/avatar"
	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/jwt"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
	"github.com/bvivg/axon/core/services/auth/internal/password"
)

type Store interface {
	CreateUserWithPassword(ctx context.Context, u domain.User, passwordHash string) (domain.User, error)
	CreateUserWithOauthAccount(ctx context.Context, u domain.User, a domain.OauthAccount) (domain.User, error)

	UserByEmail(ctx context.Context, email string) (domain.User, error)
	UserByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	UpdateProfile(ctx context.Context, id uuid.UUID, u domain.ProfileUpdate) (domain.User, error)
	SetAvatar(ctx context.Context, id uuid.UUID, avatarURL string, custom bool) (domain.User, error)

	SetCredential(ctx context.Context, userID uuid.UUID, passwordHash string) error
	CredentialByUserID(ctx context.Context, userID uuid.UUID) (domain.Credential, error)

	CreateRefreshToken(ctx context.Context, t domain.RefreshToken) error
	RefreshTokenByHash(ctx context.Context, hash string) (domain.RefreshToken, error)
	RotateRefreshToken(ctx context.Context, spentID uuid.UUID, next domain.RefreshToken, at time.Time) (bool, error)
	RevokeFamily(ctx context.Context, familyID uuid.UUID, at time.Time) (int64, error)
	RevokeFamilyForUser(ctx context.Context, userID, familyID uuid.UUID, at time.Time) (int64, error)
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) (int64, error)
	Sessions(ctx context.Context, userID uuid.UUID, at time.Time) ([]domain.Session, error)

	OauthAccountByProviderID(ctx context.Context, p domain.Provider, providerUserID string) (domain.OauthAccount, error)
	LinkOauthAccount(ctx context.Context, a domain.OauthAccount) error
}

type Config struct {
	RefreshTTL time.Duration

	OAuth *OAuthConfig

	Avatar *avatar.Pipeline

	Now func() time.Time
}

type OAuthConfig struct {
	Providers *oauth.Registry
	States    *oauth.StateStore
	ReturnTo  *oauth.ReturnToPolicy
}

type oauthDeps struct {
	providers *oauth.Registry
	states    *oauth.StateStore
	returnTo  *oauth.ReturnToPolicy
}

type Service struct {
	store  Store
	hasher *password.Hasher
	issuer *jwt.Issuer
	log    *slog.Logger

	refreshTTL time.Duration
	now        func() time.Time

	oauth  *oauthDeps
	avatar *avatar.Pipeline

	dummyHash string
}

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
		avatar:     cfg.Avatar,
		dummyHash:  dummyHash,
	}, nil
}

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
