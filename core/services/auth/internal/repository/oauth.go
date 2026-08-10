package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const oauthAccountColumns = `provider, provider_user_id, user_id, email, linked_at`

// OauthAccountByProviderID resolves a provider identity to a linked account.
func (r *Repository) OauthAccountByProviderID(ctx context.Context, provider domain.Provider, providerUserID string) (domain.OauthAccount, error) {
	const query = `
		SELECT ` + oauthAccountColumns + `
		FROM oauth_accounts
		WHERE provider = $1 AND provider_user_id = $2`

	a, err := scanOauthAccount(r.q.QueryRow(ctx, query, provider.String(), providerUserID))
	if err != nil {
		if noRows(err) {
			return domain.OauthAccount{}, domain.ErrUserNotFound
		}
		return domain.OauthAccount{}, fmt.Errorf("repository: oauth account by provider id: %w", err)
	}
	return a, nil
}

// LinkOauthAccount attaches a provider identity to a user.
//
// A repeated link for the same identity is not an error — it happens whenever
// someone signs in again — but the identity never moves to a different user:
// the WHERE clause on the update keeps it with whoever claimed it first, and an
// attempt to move it fails with domain.ErrOauthIdentityClaimed.
//
// The row count is what makes that refusal visible. A conditional DO UPDATE
// whose WHERE clause matches nothing updates no row and reports no error, so
// without this check the caller is told the link exists when it does not — and
// then acts on it.
func (r *Repository) LinkOauthAccount(ctx context.Context, a domain.OauthAccount) error {
	const query = `
		INSERT INTO oauth_accounts (provider, provider_user_id, user_id, email)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (provider, provider_user_id) DO UPDATE
		SET email = EXCLUDED.email
		WHERE oauth_accounts.user_id = EXCLUDED.user_id`

	tag, err := r.q.Exec(ctx, query, a.Provider.String(), a.ProviderUserID, a.UserID, a.Email)
	if err != nil {
		return fmt.Errorf("repository: link oauth account: %w", err)
	}

	// Zero rows has one cause here: the identity exists and belongs to somebody
	// else. An insert affects one row, and so does the update when the identity
	// is already this user's — Postgres counts it whether or not the email
	// actually changed.
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("repository: link oauth account: %w", domain.ErrOauthIdentityClaimed)
	}
	return nil
}

// OauthAccountsForUser lists the providers a user has linked, so the client can
// show what is connected.
func (r *Repository) OauthAccountsForUser(ctx context.Context, userID uuid.UUID) ([]domain.OauthAccount, error) {
	const query = `
		SELECT ` + oauthAccountColumns + `
		FROM oauth_accounts
		WHERE user_id = $1
		ORDER BY linked_at`

	rows, err := r.q.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: oauth accounts for user: %w", err)
	}
	defer rows.Close()

	var accounts []domain.OauthAccount
	for rows.Next() {
		a, err := scanOauthAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan oauth account: %w", err)
		}
		accounts = append(accounts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: oauth accounts for user: %w", err)
	}
	return accounts, nil
}

func scanOauthAccount(row scanRow) (domain.OauthAccount, error) {
	var (
		a        domain.OauthAccount
		provider string
	)

	err := row.Scan(&provider, &a.ProviderUserID, &a.UserID, &a.Email, &a.LinkedAt)
	if err != nil {
		return domain.OauthAccount{}, err
	}

	a.Provider = domain.Provider(provider)
	return a, nil
}
