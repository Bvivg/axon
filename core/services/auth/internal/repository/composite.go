package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

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

func (r *Repository) RotateRefreshToken(ctx context.Context, spentID uuid.UUID, next domain.RefreshToken, at time.Time) (bool, error) {
	var won bool

	err := r.InTx(ctx, func(tx *Repository) error {
		var err error
		won, err = tx.MarkRefreshTokenUsed(ctx, spentID, at)
		if err != nil {
			return err
		}
		if !won {

			return nil
		}
		return tx.CreateRefreshToken(ctx, next)
	})
	if err != nil {
		return false, err
	}
	return won, nil
}

func (r *Repository) SetPasswordAndRevokeSessions(ctx context.Context, userID uuid.UUID, passwordHash string, at time.Time) error {
	return r.InTx(ctx, func(tx *Repository) error {
		if err := tx.SetCredential(ctx, userID, passwordHash); err != nil {
			return err
		}
		_, err := tx.RevokeAllForUser(ctx, userID, at)
		return err
	})
}
