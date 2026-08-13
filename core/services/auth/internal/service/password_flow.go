package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

type RegisterInput struct {
	Email       string
	Password    string
	DisplayName string
	Device      domain.Device
}

type Result struct {
	User   domain.User
	Tokens domain.TokenPair
}

func (s *Service) Register(ctx context.Context, in RegisterInput) (Result, error) {
	email, err := domain.ValidateEmail(in.Email)
	if err != nil {
		return Result{}, err
	}
	if err := domain.ValidatePassword(in.Password); err != nil {
		return Result{}, err
	}
	displayName, err := domain.ValidateDisplayName(in.DisplayName)
	if err != nil {
		return Result{}, err
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return Result{}, fmt.Errorf("service: hash password: %w", err)
	}

	user, err := s.store.CreateUserWithPassword(ctx, domain.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: displayName,
	}, hash)
	if err != nil {

		return Result{}, err
	}

	tokens, err := s.issueTokens(ctx, user, uuid.New(), in.Device)
	if err != nil {
		return Result{}, err
	}

	s.log.InfoContext(ctx, "account registered", "user_id", user.ID)

	return Result{User: user, Tokens: tokens}, nil
}

type LoginInput struct {
	Email    string
	Password string
	Device   domain.Device
}

func (s *Service) Login(ctx context.Context, in LoginInput) (Result, error) {
	email := domain.NormalizeEmail(in.Email)

	user, err := s.store.UserByEmail(ctx, email)
	switch {
	case errors.Is(err, domain.ErrUserNotFound):

		if _, verifyErr := s.hasher.Verify(in.Password, s.dummyHash); verifyErr != nil {
			return Result{}, fmt.Errorf("service: verify against placeholder hash: %w", verifyErr)
		}
		return Result{}, domain.ErrInvalidCredentials
	case err != nil:
		return Result{}, err
	}

	credential, err := s.store.CredentialByUserID(ctx, user.ID)
	switch {
	case errors.Is(err, domain.ErrNoPassword):

		if _, verifyErr := s.hasher.Verify(in.Password, s.dummyHash); verifyErr != nil {
			return Result{}, fmt.Errorf("service: verify against placeholder hash: %w", verifyErr)
		}
		return Result{}, domain.ErrInvalidCredentials
	case err != nil:
		return Result{}, err
	}

	ok, err := s.hasher.Verify(in.Password, credential.PasswordHash)
	if err != nil {

		return Result{}, fmt.Errorf("service: verify password for user %s: %w", user.ID, err)
	}
	if !ok {
		return Result{}, domain.ErrInvalidCredentials
	}

	s.rehashIfNeeded(ctx, user.ID, in.Password, credential.PasswordHash)

	tokens, err := s.issueTokens(ctx, user, uuid.New(), in.Device)
	if err != nil {
		return Result{}, err
	}

	s.log.InfoContext(ctx, "password sign-in", "user_id", user.ID)

	return Result{User: user, Tokens: tokens}, nil
}

func (s *Service) rehashIfNeeded(ctx context.Context, userID uuid.UUID, plaintext, stored string) {
	if !s.hasher.NeedsRehash(stored) {
		return
	}

	upgraded, err := s.hasher.Hash(plaintext)
	if err != nil {
		s.log.WarnContext(ctx, "could not re-hash password", "user_id", userID, "error", err)
		return
	}
	if err := s.store.SetCredential(ctx, userID, upgraded); err != nil {
		s.log.WarnContext(ctx, "could not store re-hashed password", "user_id", userID, "error", err)
		return
	}

	s.log.InfoContext(ctx, "password re-hashed with current parameters", "user_id", userID)
}

func (s *Service) Me(ctx context.Context, userID uuid.UUID) (domain.User, error) {
	return s.store.UserByID(ctx, userID)
}
