package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
)

type StartOAuthResult struct {
	AuthorizationURL string

	State string
}

func (s *Service) StartOAuth(ctx context.Context, id domain.Provider, returnTo string) (StartOAuthResult, error) {
	if s.oauth == nil {
		return StartOAuthResult{}, domain.ErrProviderUnsupported
	}

	provider, err := s.oauth.providers.Get(id)
	if err != nil {
		return StartOAuthResult{}, err
	}

	destination, err := s.oauth.returnTo.Resolve(returnTo)
	if err != nil {
		return StartOAuthResult{}, err
	}

	verifier, err := oauth.NewVerifier()
	if err != nil {
		return StartOAuthResult{}, err
	}

	state, err := s.oauth.states.Put(ctx, oauth.State{
		Provider: id,
		Verifier: verifier,
		ReturnTo: destination,
	})
	if err != nil {
		return StartOAuthResult{}, err
	}

	s.log.InfoContext(ctx, "oauth sign-in started", "provider", id)

	return StartOAuthResult{
		AuthorizationURL: provider.AuthorizationURL(state, oauth.Challenge(verifier)),
		State:            state,
	}, nil
}

type CompleteOAuthResult struct {
	Result

	Created bool

	ReturnTo string
}

type CompleteOAuthInput struct {
	Provider domain.Provider
	Code     string
	State    string

	DisplayName string
}

func (s *Service) CompleteOAuth(ctx context.Context, in CompleteOAuthInput) (CompleteOAuthResult, error) {
	if s.oauth == nil {
		return CompleteOAuthResult{}, domain.ErrProviderUnsupported
	}

	id := in.Provider

	provider, err := s.oauth.providers.Get(id)
	if err != nil {
		return CompleteOAuthResult{}, err
	}

	stored, err := s.oauth.states.Take(ctx, in.State)
	if err != nil {
		return CompleteOAuthResult{}, err
	}

	if stored.Provider != id {
		s.log.WarnContext(ctx, "oauth state presented for the wrong provider",
			"issued_for", stored.Provider, "presented_for", id)
		return CompleteOAuthResult{}, domain.ErrOauthStateInvalid
	}

	profile, err := provider.Exchange(ctx, in.Code, stored.Verifier)
	if err != nil {

		if errors.Is(err, domain.ErrOauthEmailUnverified) || errors.Is(err, domain.ErrOauthProfileIncomplete) {
			return CompleteOAuthResult{}, err
		}
		s.log.ErrorContext(ctx, "oauth code exchange failed", "provider", id, "error", err)
		return CompleteOAuthResult{}, domain.ErrOauthStateInvalid
	}

	if profile.DisplayName == "" {
		profile.DisplayName = in.DisplayName
	}

	user, created, err := s.resolveOauthUser(ctx, id, profile)
	if err != nil {
		return CompleteOAuthResult{}, err
	}

	tokens, err := s.issueTokens(ctx, user, uuid.New())
	if err != nil {
		return CompleteOAuthResult{}, err
	}

	s.log.InfoContext(ctx, "oauth sign-in",
		"provider", id,
		"user_id", user.ID,
		"created", created,
		"private_email", profile.IsPrivateEmail,
	)

	return CompleteOAuthResult{
		Result:   Result{User: user, Tokens: tokens},
		Created:  created,
		ReturnTo: stored.ReturnTo,
	}, nil
}

func (s *Service) resolveOauthUser(
	ctx context.Context,
	id domain.Provider,
	profile domain.ProviderProfile,
) (domain.User, bool, error) {
	link, err := s.store.OauthAccountByProviderID(ctx, id, profile.ProviderUserID)
	switch {
	case err == nil:
		user, err := s.store.UserByID(ctx, link.UserID)
		if err != nil {
			return domain.User{}, false, err
		}
		return user, false, nil
	case !errors.Is(err, domain.ErrUserNotFound):
		return domain.User{}, false, err
	}

	email := domain.NormalizeEmail(profile.Email)
	displayName, err := domain.ValidateDisplayName(profile.DisplayName)
	if err != nil {

		displayName = ""
	}

	account := domain.OauthAccount{
		Provider:       id,
		ProviderUserID: profile.ProviderUserID,
		Email:          email,
	}

	existing, err := s.store.UserByEmail(ctx, email)
	switch {
	case err == nil:
		account.UserID = existing.ID
		if err := s.store.LinkOauthAccount(ctx, account); err != nil {

			if errors.Is(err, domain.ErrOauthIdentityClaimed) {
				s.log.WarnContext(ctx, "provider identity is already linked to another account",
					"provider", id, "user_id", existing.ID)
			}
			return domain.User{}, false, err
		}

		s.log.InfoContext(ctx, "provider linked to an existing account",
			"provider", id, "user_id", existing.ID)

		return existing, false, nil
	case !errors.Is(err, domain.ErrUserNotFound):
		return domain.User{}, false, err
	}

	user, err := s.store.CreateUserWithOauthAccount(ctx, domain.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: displayName,
		AvatarURL:   profile.AvatarURL,

		EmailVerified: true,
	}, account)
	if err != nil {
		return domain.User{}, false, fmt.Errorf("service: create account from %s profile: %w", id, err)
	}

	return user, true, nil
}
