package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

// RegisterInput is what registration needs from the caller.
type RegisterInput struct {
	Email       string
	Password    string
	DisplayName string
}

// Result is a signed-in user together with their tokens.
type Result struct {
	User   domain.User
	Tokens domain.TokenPair
}

// Register creates an account and signs it in.
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

	// The id is generated here so the user and credential rows can be written in
	// one transaction without reading it back in between.
	user, err := s.store.CreateUserWithPassword(ctx, domain.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: displayName,
	}, hash)
	if err != nil {
		// ErrEmailTaken is a real answer for registration, unlike on login: the
		// address is already visibly in use to whoever owns it, and refusing to
		// say so would just produce an account the caller cannot explain.
		return Result{}, err
	}

	tokens, err := s.issueTokens(ctx, user, uuid.New())
	if err != nil {
		return Result{}, err
	}

	s.log.InfoContext(ctx, "account registered", "user_id", user.ID)

	return Result{User: user, Tokens: tokens}, nil
}

// LoginInput is what a password sign-in needs.
type LoginInput struct {
	Email    string
	Password string
}

// Login exchanges an email and password for tokens.
//
// Every failure short of an infrastructure error returns ErrInvalidCredentials,
// and every path costs one password verification. Both are needed: matching
// error messages are pointless if the response time still says whether the
// address exists.
func (s *Service) Login(ctx context.Context, in LoginInput) (Result, error) {
	email := domain.NormalizeEmail(in.Email)

	user, err := s.store.UserByEmail(ctx, email)
	switch {
	case errors.Is(err, domain.ErrUserNotFound):
		// Spend the time a real verification would, then fail.
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
		// An account that only ever signed in through a provider. Same shape as
		// above: burn the time, then give the same answer as a wrong password, so
		// the response does not reveal how the account was created.
		if _, verifyErr := s.hasher.Verify(in.Password, s.dummyHash); verifyErr != nil {
			return Result{}, fmt.Errorf("service: verify against placeholder hash: %w", verifyErr)
		}
		return Result{}, domain.ErrInvalidCredentials
	case err != nil:
		return Result{}, err
	}

	ok, err := s.hasher.Verify(in.Password, credential.PasswordHash)
	if err != nil {
		// The stored hash is unreadable. That is an operational failure, not a
		// wrong password, and it must not be reported as one.
		return Result{}, fmt.Errorf("service: verify password for user %s: %w", user.ID, err)
	}
	if !ok {
		return Result{}, domain.ErrInvalidCredentials
	}

	// Upgrade the hash while the plaintext is in hand. This is the only moment it
	// is possible, which is why raising the cost has to be a gradual migration
	// rather than a flag day.
	s.rehashIfNeeded(ctx, user.ID, in.Password, credential.PasswordHash)

	tokens, err := s.issueTokens(ctx, user, uuid.New())
	if err != nil {
		return Result{}, err
	}

	s.log.InfoContext(ctx, "password sign-in", "user_id", user.ID)

	return Result{User: user, Tokens: tokens}, nil
}

// rehashIfNeeded re-hashes a password that was stored under weaker parameters.
//
// A failure is logged and swallowed: the sign-in already succeeded, and refusing
// it because an optimisation did not land would be the wrong trade.
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

// Me returns the profile of the authenticated user.
//
// The caller passes the subject from a verified token; there is no path that
// takes a user id from the request body.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (domain.User, error) {
	return s.store.UserByID(ctx, userID)
}
