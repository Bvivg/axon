package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

// The methods here span more than one table and so own a transaction.
//
// They exist as single operations on purpose. The alternative — exposing InTx to
// the service layer and letting it compose writes — would put an atomicity
// requirement in the layer that has no business knowing whether the store even
// has transactions. Here the service asks for "create a user with a password",
// and it is this package's problem that the request touches two tables.

// CreateUserWithPassword creates a user and its credential together.
//
// Atomic because a failure between the two would leave an account that nobody
// can sign in to and that nobody can register again either: the email is taken,
// but there is no password to check against.
func (r *Repository) CreateUserWithPassword(ctx context.Context, u domain.User, passwordHash string) (domain.User, error) {
	var created domain.User

	err := r.InTx(ctx, func(tx *Repository) error {
		var err error
		if created, err = tx.CreateUser(ctx, u); err != nil {
			return err
		}
		return tx.SetCredential(ctx, created.ID, passwordHash)
	})
	if err != nil {
		return domain.User{}, err
	}
	return created, nil
}

// CreateUserWithOauthAccount creates a user and links the provider identity that
// produced it.
//
// Atomic for the mirror-image reason: a user without the link would be
// unreachable, because the next sign-in from the same provider identity would
// find nothing and try to create the account again against a taken email.
func (r *Repository) CreateUserWithOauthAccount(ctx context.Context, u domain.User, a domain.OauthAccount) (domain.User, error) {
	var created domain.User

	err := r.InTx(ctx, func(tx *Repository) error {
		var err error
		if created, err = tx.CreateUser(ctx, u); err != nil {
			return err
		}

		a.UserID = created.ID
		return tx.LinkOauthAccount(ctx, a)
	})
	if err != nil {
		return domain.User{}, err
	}
	return created, nil
}

// RotateRefreshToken spends the presented token and issues its successor,
// reporting whether the rotation was the one that won.
//
// A false return is not an error: it means the token had already been spent by
// the time this transaction got to it, which the caller must treat as reuse.
// Doing both writes in one transaction is what makes that answer trustworthy —
// otherwise two concurrent refreshes could each mark the token used, fail to
// notice, and hand out two live successors from one token.
func (r *Repository) RotateRefreshToken(ctx context.Context, spentID uuid.UUID, next domain.RefreshToken, at time.Time) (bool, error) {
	var won bool

	err := r.InTx(ctx, func(tx *Repository) error {
		var err error
		won, err = tx.MarkRefreshTokenUsed(ctx, spentID, at)
		if err != nil {
			return err
		}
		if !won {
			// Nothing is issued, and the transaction commits with no changes so
			// the caller can act on `won` rather than on an error.
			return nil
		}
		return tx.CreateRefreshToken(ctx, next)
	})
	if err != nil {
		return false, err
	}
	return won, nil
}

// SetPasswordAndRevokeSessions changes a password and signs every existing
// session out.
//
// Atomic because the two halves are one intent: a password change that leaves
// old refresh tokens alive has not actually locked anyone out, which is usually
// the entire reason someone changes it.
func (r *Repository) SetPasswordAndRevokeSessions(ctx context.Context, userID uuid.UUID, passwordHash string, at time.Time) error {
	return r.InTx(ctx, func(tx *Repository) error {
		if err := tx.SetCredential(ctx, userID, passwordHash); err != nil {
			return err
		}
		_, err := tx.RevokeAllForUser(ctx, userID, at)
		return err
	})
}
