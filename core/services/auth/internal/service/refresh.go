package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/token"
)

// Refresh exchanges a refresh token for a new pair.
//
// The rotation is what makes a stolen refresh token detectable. Each exchange
// spends the presented token and issues a successor in the same family, so a
// token can legitimately be used exactly once. If a spent one comes back, there
// are two live holders of the same chain and no way to tell which is the thief —
// so the entire family is revoked and both are signed out. Making the real user
// sign in again is a far better outcome than leaving an attacker with a session.
//
// Detection has to cover both orderings. A token presented after it was already
// exchanged is caught by the state check; two exchanges racing each other are
// caught by the rotation reporting which one won. Without the second case, two
// concurrent requests with one stolen token could each hand out a live successor.
func (s *Service) Refresh(ctx context.Context, presented string) (Result, error) {
	if presented == "" {
		// An empty value is not a real token, and not worth telling apart from
		// one that simply does not exist.
		return Result{}, domain.ErrRefreshTokenInvalid
	}

	hash, err := token.Hash(presented)
	if err != nil {
		return Result{}, domain.ErrRefreshTokenInvalid
	}

	stored, err := s.store.RefreshTokenByHash(ctx, hash)
	if err != nil {
		// Unknown tokens arrive as ErrRefreshTokenInvalid from the store.
		return Result{}, err
	}

	now := s.now().UTC()

	// Already spent: this is the replay case.
	if stored.Used() {
		s.revokeCompromisedFamily(ctx, stored, "a spent refresh token was presented again")
		return Result{}, domain.ErrRefreshTokenReused
	}

	// Revoked or expired is ordinary rejection, not a security event. Revoked
	// covers the token the legitimate user still held when a family was killed,
	// so it must not itself trigger another revocation.
	if stored.Revoked() || stored.Expired(now) {
		return Result{}, domain.ErrRefreshTokenInvalid
	}

	next, err := token.New()
	if err != nil {
		return Result{}, fmt.Errorf("service: generate refresh token: %w", err)
	}

	successor := domain.RefreshToken{
		ID:        uuid.New(),
		UserID:    stored.UserID,
		FamilyID:  stored.FamilyID,
		TokenHash: next.Hash,
		ExpiresAt: now.Add(s.refreshTTL),
	}

	won, err := s.store.RotateRefreshToken(ctx, stored.ID, successor, now)
	if err != nil {
		return Result{}, err
	}
	if !won {
		// Someone else spent this token between the read above and the write. One
		// of the two is not the owner; treat it the same as a replay.
		s.revokeCompromisedFamily(ctx, stored, "two exchanges raced for one refresh token")
		return Result{}, domain.ErrRefreshTokenReused
	}

	user, err := s.store.UserByID(ctx, stored.UserID)
	if err != nil {
		return Result{}, err
	}

	access, expiresAt, err := s.issuer.Issue(user.ID, user.Email)
	if err != nil {
		return Result{}, err
	}

	return Result{
		User: user,
		Tokens: domain.TokenPair{
			AccessToken:     access,
			RefreshToken:    next.Value,
			AccessExpiresAt: expiresAt,
		},
	}, nil
}

// revokeCompromisedFamily kills every token descended from the same sign-in and
// records it at warn level.
//
// The log line matters as much as the revocation: this is the signal that a
// token leaked, and it should be visible without anyone going looking for it.
// A failure to revoke is logged at error and not returned — the caller is being
// refused either way, and reporting the storage failure instead would tell an
// attacker their replay hit something unexpected.
func (s *Service) revokeCompromisedFamily(ctx context.Context, t domain.RefreshToken, reason string) {
	revoked, err := s.store.RevokeFamily(ctx, t.FamilyID, s.now().UTC())
	if err != nil {
		s.log.ErrorContext(ctx, "could not revoke a compromised refresh token family",
			"user_id", t.UserID,
			"family_id", t.FamilyID,
			"error", err,
		)
		return
	}

	s.log.WarnContext(ctx, "refresh token reuse detected, family revoked",
		"user_id", t.UserID,
		"family_id", t.FamilyID,
		"tokens_revoked", revoked,
		"reason", reason,
	)
}

// Logout revokes the family the presented token belongs to, signing that
// sign-in out everywhere it was refreshed to.
//
// It is idempotent and says nothing about whether the token existed. A logout
// that reported "no such token" would be a way to probe which tokens are live.
func (s *Service) Logout(ctx context.Context, presented string) error {
	if presented == "" {
		// Nothing to revoke, and nothing to report: the caller is already signed
		// out as far as this token is concerned.
		return nil
	}

	hash, err := token.Hash(presented)
	if err != nil {
		return err
	}

	stored, err := s.store.RefreshTokenByHash(ctx, hash)
	switch {
	case errors.Is(err, domain.ErrRefreshTokenInvalid):
		return nil
	case err != nil:
		return err
	}

	revoked, err := s.store.RevokeFamily(ctx, stored.FamilyID, s.now().UTC())
	if err != nil {
		return err
	}

	s.log.InfoContext(ctx, "signed out",
		"user_id", stored.UserID,
		"family_id", stored.FamilyID,
		"tokens_revoked", revoked,
	)
	return nil
}

// LogoutEverywhere revokes every live token a user has, across all sign-ins.
func (s *Service) LogoutEverywhere(ctx context.Context, userID uuid.UUID) error {
	revoked, err := s.store.RevokeAllForUser(ctx, userID, s.now().UTC())
	if err != nil {
		return err
	}

	s.log.InfoContext(ctx, "signed out everywhere", "user_id", userID, "tokens_revoked", revoked)
	return nil
}

// issueTokens starts a new refresh family and issues the first pair in it.
//
// A fresh family per sign-in is deliberate: two devices are two chains, so
// revoking one because its token was replayed does not sign the other out.
func (s *Service) issueTokens(ctx context.Context, user domain.User, familyID uuid.UUID) (domain.TokenPair, error) {
	refresh, err := token.New()
	if err != nil {
		return domain.TokenPair{}, fmt.Errorf("service: generate refresh token: %w", err)
	}

	now := s.now().UTC()

	err = s.store.CreateRefreshToken(ctx, domain.RefreshToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		FamilyID:  familyID,
		TokenHash: refresh.Hash,
		ExpiresAt: now.Add(s.refreshTTL),
	})
	if err != nil {
		return domain.TokenPair{}, err
	}

	access, expiresAt, err := s.issuer.Issue(user.ID, user.Email)
	if err != nil {
		return domain.TokenPair{}, err
	}

	return domain.TokenPair{
		AccessToken:     access,
		RefreshToken:    refresh.Value,
		AccessExpiresAt: expiresAt,
	}, nil
}
