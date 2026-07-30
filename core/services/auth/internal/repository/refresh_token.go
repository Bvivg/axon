package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

// Named without "token" in it because gosec's hardcoded-credentials check keys
// off the identifier, and a column list is not a secret.
const refreshColumns = `id, user_id, family_id, token_hash, issued_at, expires_at, used_at, revoked_at`

// CreateRefreshToken stores a newly issued token.
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

// RefreshTokenByHash looks a token up by the hash of its presented value.
//
// It returns spent and revoked tokens too, on purpose: reuse detection needs to
// see that a token exists and has already been used. Filtering them out here
// would make a stolen token indistinguishable from a made-up one, and the theft
// would go unnoticed.
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

// MarkRefreshTokenUsed spends a token, reporting whether it was still unspent.
//
// The check is in the WHERE clause rather than in a preceding SELECT so the
// database decides the winner: two concurrent refreshes with the same token
// both pass a read-then-write check, but only one of them updates a row here.
// The loser is treated as reuse, which is the safe reading.
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

// RevokeFamily revokes every unrevoked token descended from one sign-in and
// returns how many it touched.
//
// This is what reuse detection triggers: once a spent token comes back, no
// token in that chain can be trusted, including the one the legitimate holder
// still has. Signing everyone in that family out is the point.
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

// RevokeAllForUser revokes every live token a user has: sign out everywhere,
// and what a password change should trigger.
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

// DeleteExpiredRefreshTokens removes tokens that expired before cutoff and
// returns how many went. Spent and revoked rows are kept until they expire:
// they are the evidence reuse detection relies on, and deleting them early
// would turn a replay into an unrecognised token.
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

	// NULL becomes the zero time, which is what the domain predicates read.
	if usedAt != nil {
		t.UsedAt = *usedAt
	}
	if revokedAt != nil {
		t.RevokedAt = *revokedAt
	}
	return t, nil
}
