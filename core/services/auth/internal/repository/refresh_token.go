package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const refreshColumns = `id, user_id, family_id, token_hash, issued_at, expires_at, used_at, revoked_at`

func (r *Repository) CreateRefreshToken(ctx context.Context, t domain.RefreshToken) error {
	const query = `
		INSERT INTO refresh_tokens (id, user_id, family_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5)`

	_, err := r.q.Exec(ctx, query, t.ID, t.UserID, t.FamilyID, t.TokenHash, t.ExpiresAt)
	if err != nil {
		return fmt.Errorf("repository: create refresh token: %w", err)
	}
	return nil
}

func (r *Repository) RefreshTokenByHash(ctx context.Context, hash string) (domain.RefreshToken, error) {
	const query = `SELECT ` + refreshColumns + ` FROM refresh_tokens WHERE token_hash = $1`

	t, err := scanRefreshToken(r.q.QueryRow(ctx, query, hash))
	if err != nil {
		if noRows(err) {
			return domain.RefreshToken{}, domain.ErrRefreshTokenInvalid
		}
		return domain.RefreshToken{}, fmt.Errorf("repository: refresh token by hash: %w", err)
	}
	return t, nil
}

func (r *Repository) MarkRefreshTokenUsed(ctx context.Context, id uuid.UUID, at time.Time) (bool, error) {
	const query = `
		UPDATE refresh_tokens
		SET used_at = $2
		WHERE id = $1 AND used_at IS NULL AND revoked_at IS NULL`

	tag, err := r.q.Exec(ctx, query, id, at)
	if err != nil {
		return false, fmt.Errorf("repository: mark refresh token used: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) RevokeFamily(ctx context.Context, familyID uuid.UUID, at time.Time) (int64, error) {
	const query = `
		UPDATE refresh_tokens
		SET revoked_at = $2
		WHERE family_id = $1 AND revoked_at IS NULL`

	tag, err := r.q.Exec(ctx, query, familyID, at)
	if err != nil {
		return 0, fmt.Errorf("repository: revoke refresh token family: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) (int64, error) {
	const query = `
		UPDATE refresh_tokens
		SET revoked_at = $2
		WHERE user_id = $1 AND revoked_at IS NULL`

	tag, err := r.q.Exec(ctx, query, userID, at)
	if err != nil {
		return 0, fmt.Errorf("repository: revoke user refresh tokens: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) DeleteExpiredRefreshTokens(ctx context.Context, cutoff time.Time) (int64, error) {
	const query = `DELETE FROM refresh_tokens WHERE expires_at < $1`

	tag, err := r.q.Exec(ctx, query, cutoff)
	if err != nil {
		return 0, fmt.Errorf("repository: delete expired refresh tokens: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanRefreshToken(row scanRow) (domain.RefreshToken, error) {
	var (
		t         domain.RefreshToken
		usedAt    *time.Time
		revokedAt *time.Time
	)

	err := row.Scan(
		&t.ID,
		&t.UserID,
		&t.FamilyID,
		&t.TokenHash,
		&t.IssuedAt,
		&t.ExpiresAt,
		&usedAt,
		&revokedAt,
	)
	if err != nil {
		return domain.RefreshToken{}, err
	}

	if usedAt != nil {
		t.UsedAt = *usedAt
	}
	if revokedAt != nil {
		t.RevokedAt = *revokedAt
	}
	return t, nil
}
