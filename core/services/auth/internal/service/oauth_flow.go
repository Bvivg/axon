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

func (s *Service) StartOAuth(ctx context.Context, id domain.Provider, returnTo string, linkUserID *uuid.UUID) (StartOAuthResult, error) {
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
		Provider:   id,
		Verifier:   verifier,
		ReturnTo:   destination,
		LinkUserID: linkUserID,
	})
	if err != nil {
		return StartOAuthResult{}, err
	}

	if linkUserID != nil {
		s.log.InfoContext(ctx, "oauth linking started", "provider", id, "user_id", *linkUserID)
	} else {
		s.log.InfoContext(ctx, "oauth sign-in started", "provider", id)
	}

	return StartOAuthResult{
		AuthorizationURL: provider.AuthorizationURL(state, oauth.Challenge(verifier)),
		State:            state,
	}, nil
}

type CompleteOAuthResult struct {
	Result

	Created bool

	Linked bool

	ReturnTo string
}

type CompleteOAuthInput struct {
	Provider domain.Provider
	Code     string
	State    string

	DisplayName string

	Device domain.Device

	CallerID *uuid.UUID
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

	if stored.LinkUserID != nil && (in.CallerID == nil || *in.CallerID != *stored.LinkUserID) {
		s.log.WarnContext(ctx, "oauth linking completed by someone other than the account that started it",
			"provider", id, "user_id", *stored.LinkUserID)
		return CompleteOAuthResult{}, domain.ErrOauthStateInvalid
	}

	profile, err := provider.Exchange(ctx, in.Code, stored.Verifier)
	if err != nil {

		if errors.Is(err, domain.ErrOauthEmailUnverified) || errors.Is(err, domain.ErrOauthProfileIncomplete) ||
			errors.Is(err, domain.ErrOauthEmailNotAllowed) {
			return CompleteOAuthResult{}, err
		}
		s.log.ErrorContext(ctx, "oauth code exchange failed", "provider", id, "error", err)
		return CompleteOAuthResult{}, domain.ErrOauthStateInvalid
	}

	if stored.LinkUserID != nil {
		return s.linkProvider(ctx, *stored.LinkUserID, id, profile, stored.ReturnTo)
	}

	if profile.DisplayName == "" {
		profile.DisplayName = in.DisplayName
	}

	user, created, err := s.resolveOauthUser(ctx, id, profile)
	if err != nil {
		return CompleteOAuthResult{}, err
	}

	user = s.importProviderAvatar(ctx, user, profile.AvatarURL)

	tokens, err := s.issueTokens(ctx, user, uuid.New(), in.Device)
	if err != nil {
		return CompleteOAuthResult{}, err
	}

	s.log.InfoContext(ctx, "oauth sign-in",
		"provider", id,
		"user_id", user.ID,
		"created", created,
		"private_email", profile.IsPrivateEmail,
	)

	s.audit(ctx, domain.SecurityEvent{
		Action:  domain.SecurityLoginSucceeded,
		UserID:  user.ID,
		ActorID: user.ID,
		Subject: id.String(),
	})

	return CompleteOAuthResult{
		Result:   Result{User: s.attachRoles(ctx, user), Tokens: tokens},
		Created:  created,
		ReturnTo: stored.ReturnTo,
	}, nil
}

func (s *Service) linkProvider(
	ctx context.Context,
	userID uuid.UUID,
	id domain.Provider,
	profile domain.ProviderProfile,
	returnTo string,
) (CompleteOAuthResult, error) {
	user, err := s.store.UserByID(ctx, userID)
	if err != nil {
		return CompleteOAuthResult{}, err
	}

	if err := s.store.LinkOauthAccount(ctx, domain.OauthAccount{
		Provider:       id,
		ProviderUserID: profile.ProviderUserID,
		UserID:         userID,
		Email:          domain.NormalizeEmail(profile.Email),
	}); err != nil {
		if errors.Is(err, domain.ErrOauthIdentityClaimed) {
			s.log.WarnContext(ctx, "provider identity is already linked to another account",
				"provider", id, "user_id", userID)
		}
		return CompleteOAuthResult{}, err
	}

	s.log.InfoContext(ctx, "provider linked by its owner", "provider", id, "user_id", userID)

	s.audit(ctx, domain.SecurityEvent{
		Action:  domain.SecurityProviderLinked,
		UserID:  userID,
		ActorID: userID,
		Subject: id.String(),
	})

	return CompleteOAuthResult{
		Result:   Result{User: s.attachRoles(ctx, user)},
		Linked:   true,
		ReturnTo: returnTo,
	}, nil
}

func (s *Service) LinkedProviders(ctx context.Context, userID uuid.UUID) ([]domain.Provider, error) {
	accounts, err := s.store.OauthAccountsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	seen := make(map[domain.Provider]struct{}, len(accounts))
	providers := make([]domain.Provider, 0, len(accounts))
	for _, account := range accounts {
		if _, ok := seen[account.Provider]; ok {
			continue
		}
		seen[account.Provider] = struct{}{}
		providers = append(providers, account.Provider)
	}
	return providers, nil
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
		if id == domain.ProviderFake || !existing.EmailVerified {
			s.log.WarnContext(ctx, "provider sign-in refused: the address belongs to an account it may not be linked to",
				"provider", id, "user_id", existing.ID, "email_verified", existing.EmailVerified)
			return domain.User{}, false, domain.ErrOauthAccountExists
		}

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

	user, err := s.createWithUsername(ctx, "", email, func(username string) (domain.User, error) {
		return s.store.CreateUserWithOauthAccount(ctx, domain.User{
			ID:            uuid.New(),
			Email:         email,
			Username:      username,
			DisplayName:   displayName,
			EmailVerified: true,
		}, account)
	})
	if err != nil {
		return domain.User{}, false, fmt.Errorf("service: create account from %s profile: %w", id, err)
	}

	return user, true, nil
}
