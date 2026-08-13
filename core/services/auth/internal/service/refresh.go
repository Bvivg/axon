package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/token"
)

func (s *Service) Refresh(ctx context.Context, presented string, device domain.Device) (Result, error) {
	if presented == "" {

		return Result{}, domain.ErrRefreshTokenInvalid
	}

	hash, err := token.Hash(presented)
	if err != nil {
		return Result{}, domain.ErrRefreshTokenInvalid
	}

	stored, err := s.store.RefreshTokenByHash(ctx, hash)
	if err != nil {

		return Result{}, err
	}

	now := s.now().UTC()

	if stored.Used() {
		s.revokeCompromisedFamily(ctx, stored, "a spent refresh token was presented again")
		return Result{}, domain.ErrRefreshTokenReused
	}

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
		UserAgent: device.UserAgent,
		IP:        device.IP,
	}

	won, err := s.store.RotateRefreshToken(ctx, stored.ID, successor, now)
	if err != nil {
		return Result{}, err
	}
	if !won {

		s.revokeCompromisedFamily(ctx, stored, "two exchanges raced for one refresh token")
		return Result{}, domain.ErrRefreshTokenReused
	}

	user, err := s.store.UserByID(ctx, stored.UserID)
	if err != nil {
		return Result{}, err
	}

	access, expiresAt, err := s.issuer.Issue(user.ID, user.Email, stored.FamilyID)
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

func (s *Service) Logout(ctx context.Context, presented string) error {
	if presented == "" {

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

func (s *Service) LogoutEverywhere(ctx context.Context, userID uuid.UUID) error {
	revoked, err := s.store.RevokeAllForUser(ctx, userID, s.now().UTC())
	if err != nil {
		return err
	}

	s.log.InfoContext(ctx, "signed out everywhere", "user_id", userID, "tokens_revoked", revoked)
	return nil
}

func (s *Service) issueTokens(
	ctx context.Context, user domain.User, familyID uuid.UUID, device domain.Device,
) (domain.TokenPair, error) {
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
		UserAgent: device.UserAgent,
		IP:        device.IP,
	})
	if err != nil {
		return domain.TokenPair{}, err
	}

	access, expiresAt, err := s.issuer.Issue(user.ID, user.Email, familyID)
	if err != nil {
		return domain.TokenPair{}, err
	}

	return domain.TokenPair{
		AccessToken:     access,
		RefreshToken:    refresh.Value,
		AccessExpiresAt: expiresAt,
	}, nil
}
