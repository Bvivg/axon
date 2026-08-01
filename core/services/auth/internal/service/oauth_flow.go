package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
)

// StartOAuthResult is what a client needs to begin a provider sign-in.
type StartOAuthResult struct {
	// AuthorizationURL is where the browser goes next.
	AuthorizationURL string

	// State must come back on the callback. It is single-use.
	State string
}

// StartOAuth begins an authorization code flow.
func (s *Service) StartOAuth(ctx context.Context, id domain.Provider, returnTo string) (StartOAuthResult, error) {
	if s.oauth == nil {
		return StartOAuthResult{}, domain.ErrProviderUnsupported
	}

	provider, err := s.oauth.providers.Get(id)
	if err != nil {
		return StartOAuthResult{}, err
	}

	// Checked here rather than on the callback: the destination is settled
	// before the browser leaves, and the callback then uses what was stored
	// instead of anything it was handed.
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

// CompleteOAuthResult is a finished provider sign-in.
type CompleteOAuthResult struct {
	Result

	// Created is true when this call made the account rather than signing in to
	// one that already existed.
	Created bool

	// ReturnTo is the destination agreed when the flow started, already checked
	// against the allow-list.
	ReturnTo string
}

// CompleteOAuth exchanges an authorization code for a signed-in session.
func (s *Service) CompleteOAuth(ctx context.Context, id domain.Provider, code, state string) (CompleteOAuthResult, error) {
	if s.oauth == nil {
		return CompleteOAuthResult{}, domain.ErrProviderUnsupported
	}

	provider, err := s.oauth.providers.Get(id)
	if err != nil {
		return CompleteOAuthResult{}, err
	}

	// Single-use: a state that comes back twice is a replay, and the second
	// attempt finds nothing.
	stored, err := s.oauth.states.Take(ctx, state)
	if err != nil {
		return CompleteOAuthResult{}, err
	}

	// A state minted for one provider must not close a flow at another.
	// Otherwise someone holding a state for a provider they control can spend it
	// against a provider they do not.
	if stored.Provider != id {
		s.log.WarnContext(ctx, "oauth state presented for the wrong provider",
			"issued_for", stored.Provider, "presented_for", id)
		return CompleteOAuthResult{}, domain.ErrOauthStateInvalid
	}

	profile, err := provider.Exchange(ctx, code, stored.Verifier)
	if err != nil {
		// The provider's own message can quote the code, so it is logged and
		// not returned. The two cases a client can act on pass through.
		if errors.Is(err, domain.ErrOauthEmailUnverified) || errors.Is(err, domain.ErrOauthProfileIncomplete) {
			return CompleteOAuthResult{}, err
		}
		s.log.ErrorContext(ctx, "oauth code exchange failed", "provider", id, "error", err)
		return CompleteOAuthResult{}, domain.ErrOauthStateInvalid
	}

	user, created, err := s.resolveOauthUser(ctx, id, profile)
	if err != nil {
		return CompleteOAuthResult{}, err
	}

	tokens, err := s.issueTokens(ctx, user, uuid.New())
	if err != nil {
		return CompleteOAuthResult{}, err
	}

	s.log.InfoContext(ctx, "oauth sign-in", "provider", id, "user_id", user.ID, "created", created)

	return CompleteOAuthResult{
		Result:   Result{User: user, Tokens: tokens},
		Created:  created,
		ReturnTo: stored.ReturnTo,
	}, nil
}

// resolveOauthUser finds or creates the account behind a verified profile.
//
// Three cases, in order of how much they are trusted:
//
//  1. The provider identity is already linked. Nothing to decide.
//  2. No link, but the address belongs to an existing account. The two are
//     joined. This is only safe because the profile reached here at all: the
//     provider verified the address, so it is the same person. Without that
//     check this branch is an account takeover — sign up at a lax provider as
//     victim@example.com and inherit their account.
//  3. Neither. A new account, with no password: they signed in with a provider
//     and never chose one.
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
		// A provider's display name is not the caller's input and must not fail
		// a sign-in. An unusable one is simply dropped.
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
		// The provider checked the address; recording that saves asking the
		// person to prove something that has already been proven.
		EmailVerified: true,
	}, account)
	if err != nil {
		return domain.User{}, false, fmt.Errorf("service: create account from %s profile: %w", id, err)
	}

	return user, true, nil
}
