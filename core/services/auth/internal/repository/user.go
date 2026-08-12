package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const userColumns = `id, email, email_verified, display_name, avatar_url, ` +
	`first_name, last_name, nickname, avatar_is_custom, created_at, updated_at`

func (r *Repository) CreateUser(ctx context.Context, u domain.User) (domain.User, error) {
	const query = `
		INSERT INTO users (id, email, email_verified, display_name, avatar_url)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + userColumns

	row := r.q.QueryRow(ctx, query, u.ID, u.Email, u.EmailVerified, u.DisplayName, u.AvatarURL)

	created, err := scanUser(row)
	if err != nil {
		if isUniqueViolation(err, "users_email_key") {
			return domain.User{}, domain.ErrEmailTaken
		}
		return domain.User{}, fmt.Errorf("repository: create user: %w", err)
	}
	return created, nil
}

func (r *Repository) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	const query = `SELECT ` + userColumns + ` FROM users WHERE email = $1`

	u, err := scanUser(r.q.QueryRow(ctx, query, email))
	if err != nil {
		if noRows(err) {
			return domain.User{}, domain.ErrUserNotFound
		}
		return domain.User{}, fmt.Errorf("repository: user by email: %w", err)
	}
	return u, nil
}

func (r *Repository) UserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	const query = `SELECT ` + userColumns + ` FROM users WHERE id = $1`

	u, err := scanUser(r.q.QueryRow(ctx, query, id))
	if err != nil {
		if noRows(err) {
			return domain.User{}, domain.ErrUserNotFound
		}
		return domain.User{}, fmt.Errorf("repository: user by id: %w", err)
	}
	return u, nil
}

func (r *Repository) UpdateProfile(ctx context.Context, id uuid.UUID, u domain.ProfileUpdate) (domain.User, error) {
	const query = `
		UPDATE users
		SET email          = $2,
		    email_verified = $3,
		    first_name     = $4,
		    last_name      = $5,
		    nickname       = $6,
		    display_name   = $7,
		    updated_at     = now()
		WHERE id = $1
		RETURNING ` + userColumns

	row := r.q.QueryRow(ctx, query,
		id, u.Email, u.EmailVerified, u.FirstName, u.LastName, u.Nickname, u.DisplayName)

	updated, err := scanUser(row)
	if err != nil {
		if noRows(err) {
			return domain.User{}, domain.ErrUserNotFound
		}
		if isUniqueViolation(err, "users_email_key") {
			return domain.User{}, domain.ErrEmailTaken
		}
		return domain.User{}, fmt.Errorf("repository: update profile: %w", err)
	}
	return updated, nil
}

func (r *Repository) SetAvatar(ctx context.Context, id uuid.UUID, avatarURL string, custom bool) (domain.User, error) {
	const query = `
		UPDATE users
		SET avatar_url       = $2,
		    avatar_is_custom = $3,
		    updated_at       = now()
		WHERE id = $1 AND ($3 = true OR avatar_is_custom = false)
		RETURNING ` + userColumns

	row := r.q.QueryRow(ctx, query, id, avatarURL, custom)

	updated, err := scanUser(row)
	if err != nil {
		if noRows(err) {

			return domain.User{}, domain.ErrUserNotFound
		}
		return domain.User{}, fmt.Errorf("repository: set avatar: %w", err)
	}
	return updated, nil
}

func (r *Repository) MarkEmailVerified(ctx context.Context, id uuid.UUID) error {
	const query = `UPDATE users SET email_verified = true, updated_at = now() WHERE id = $1`

	tag, err := r.q.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("repository: mark email verified: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (r *Repository) SetCredential(ctx context.Context, userID uuid.UUID, passwordHash string) error {
	const query = `
		INSERT INTO credentials (user_id, password_hash)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE
		SET password_hash = EXCLUDED.password_hash,
		    updated_at    = now()`

	if _, err := r.q.Exec(ctx, query, userID, passwordHash); err != nil {
		return fmt.Errorf("repository: set credential: %w", err)
	}
	return nil
}

func (r *Repository) CredentialByUserID(ctx context.Context, userID uuid.UUID) (domain.Credential, error) {
	const query = `SELECT user_id, password_hash, updated_at FROM credentials WHERE user_id = $1`

	var c domain.Credential
	err := r.q.QueryRow(ctx, query, userID).Scan(&c.UserID, &c.PasswordHash, &c.UpdatedAt)
	if err != nil {
		if noRows(err) {
			return domain.Credential{}, domain.ErrNoPassword
		}
		return domain.Credential{}, fmt.Errorf("repository: credential by user id: %w", err)
	}
	return c, nil
}

type scanRow interface {
	Scan(dest ...any) error
}

func scanUser(row scanRow) (domain.User, error) {
	var u domain.User
	err := row.Scan(
		&u.ID,
		&u.Email,
		&u.EmailVerified,
		&u.DisplayName,
		&u.AvatarURL,
		&u.FirstName,
		&u.LastName,
		&u.Nickname,
		&u.AvatarIsCustom,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	return u, err
}
