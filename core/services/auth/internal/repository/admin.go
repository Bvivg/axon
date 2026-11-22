package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
	"github.com/bvivg/axon/core/shared/pkg/postgres"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func (r *Repository) Permissions(ctx context.Context, userID uuid.UUID) ([]string, error) {
	const query = `
		WITH RECURSIVE granted AS (
			SELECT p.id, p.namespace, p.action
			FROM user_roles ur
			JOIN users u ON u.id = ur.user_id AND u.suspended_at IS NULL
			JOIN role_permissions rp ON rp.role_id = ur.role_id
			JOIN permissions p ON p.id = rp.permission_id
			WHERE ur.user_id = $1
			UNION
			SELECT c.id, c.namespace, c.action
			FROM permissions c
			JOIN granted g ON c.parent_id = g.id
		)
		SELECT DISTINCT namespace || ':' || action FROM granted ORDER BY 1`

	rows, err := r.q.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: permissions: %w", err)
	}
	defer rows.Close()

	permissions := []string{}
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return nil, fmt.Errorf("repository: scan permission: %w", err)
		}
		permissions = append(permissions, permission)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: permissions: %w", err)
	}
	return permissions, nil
}

type userCursor struct {
	Created time.Time `json:"t"`
	Email   string    `json:"e,omitempty"`
	ID      uuid.UUID `json:"i"`
}

func encodeCursor(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(s string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(raw, v) != nil {
		return &domain.ValidationError{Field: "cursor", Reason: "is not valid"}
	}
	return nil
}

type where struct {
	clauses []string
	args    []any
}

func (w *where) add(clause string, args ...any) {
	for _, arg := range args {
		w.args = append(w.args, arg)
		clause = strings.Replace(clause, "?", "$"+strconv.Itoa(len(w.args)), 1)
	}
	w.clauses = append(w.clauses, clause)
}

func (w *where) sql() string {
	if len(w.clauses) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.clauses, " AND ")
}

const adminUserColumns = `u.id, u.email, u.email_verified, u.username, u.display_name, u.avatar_url, ` +
	`u.first_name, u.last_name, u.nickname, u.avatar_is_custom, u.created_at, u.updated_at, u.last_seen_at, ` +
	`u.suspended_at, u.suspended_reason`

func (r *Repository) ListUsers(ctx context.Context, f domain.UserFilter, at time.Time) (domain.UserPage, error) {
	var filter where
	if q := strings.TrimSpace(f.Query); q != "" {
		pattern := postgres.Containing(q)
		filter.add(`(u.email ILIKE ? OR u.username ILIKE ? OR u.display_name ILIKE ?)`, pattern, pattern, pattern)
	}
	if f.Role != "" {
		filter.add(`EXISTS (SELECT 1 FROM user_roles fr JOIN roles rr ON rr.id = fr.role_id WHERE fr.user_id = u.id AND rr.name = ?)`, f.Role)
	}
	switch f.Status {
	case domain.UserStatusActive:
		filter.add(`u.suspended_at IS NULL`)
	case domain.UserStatusSuspended:
		filter.add(`u.suspended_at IS NOT NULL`)
	}

	var total int64
	if err := r.q.QueryRow(ctx, `SELECT count(*) FROM users u`+filter.sql(), filter.args...).Scan(&total); err != nil {
		return domain.UserPage{}, fmt.Errorf("repository: count users: %w", err)
	}

	page := filter
	order := `u.created_at DESC, u.id DESC`
	if f.Cursor != "" {
		var c userCursor
		if err := decodeCursor(f.Cursor, &c); err != nil {
			return domain.UserPage{}, err
		}
		switch f.Sort {
		case domain.UserSortOldest:
			page.add(`(u.created_at, u.id) > (?, ?)`, c.Created, c.ID)
		case domain.UserSortEmail:
			page.add(`u.email > ?`, c.Email)
		default:
			page.add(`(u.created_at, u.id) < (?, ?)`, c.Created, c.ID)
		}
	}
	switch f.Sort {
	case domain.UserSortOldest:
		order = `u.created_at, u.id`
	case domain.UserSortEmail:
		order = `u.email`
	}

	limit := domain.PageSize(f.Limit)
	page.args = append(page.args, at, limit+1)
	atArg, limitArg := len(page.args)-1, len(page.args)
	query := `SELECT ` + adminUserColumns + `,
			ARRAY(SELECT r.name FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = u.id ORDER BY r.name),
			(SELECT count(DISTINCT t.family_id) FROM refresh_tokens t
				WHERE t.user_id = u.id AND t.revoked_at IS NULL AND t.expires_at > $` + strconv.Itoa(atArg) + `),
			u.version
		FROM users u` + page.sql() + `
		ORDER BY ` + order + `
		LIMIT $` + strconv.Itoa(limitArg)

	rows, err := r.q.Query(ctx, query, page.args...)
	if err != nil {
		return domain.UserPage{}, fmt.Errorf("repository: list users: %w", err)
	}
	defer rows.Close()

	out := domain.UserPage{Total: total, Users: []domain.AdminUser{}}
	for rows.Next() {
		var (
			roles    []string
			sessions int
			version  int64
		)
		u, err := scanUser(rows, &roles, &sessions, &version)
		if err != nil {
			return domain.UserPage{}, fmt.Errorf("repository: scan user: %w", err)
		}
		u.Roles = roles
		out.Users = append(out.Users, domain.AdminUser{User: u, ActiveSessions: sessions, Version: version})
	}
	if err := rows.Err(); err != nil {
		return domain.UserPage{}, fmt.Errorf("repository: list users: %w", err)
	}

	if len(out.Users) > limit {
		out.Users = out.Users[:limit]
		last := out.Users[limit-1].User
		out.Next = encodeCursor(userCursor{Created: last.CreatedAt, Email: last.Email, ID: last.ID})
	}
	return out, nil
}

func (r *Repository) AdminUser(ctx context.Context, id uuid.UUID, at time.Time) (domain.AdminUser, error) {
	const query = `SELECT ` + adminUserColumns + `,
			ARRAY(SELECT r.name FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = u.id ORDER BY r.name),
			(SELECT count(DISTINCT t.family_id) FROM refresh_tokens t
				WHERE t.user_id = u.id AND t.revoked_at IS NULL AND t.expires_at > $2),
			u.version
		FROM users u WHERE u.id = $1`

	var (
		roles    []string
		sessions int
		version  int64
	)
	u, err := scanUser(r.q.QueryRow(ctx, query, id, at), &roles, &sessions, &version)
	if err != nil {
		if noRows(err) {
			return domain.AdminUser{}, domain.ErrUserNotFound
		}
		return domain.AdminUser{}, fmt.Errorf("repository: admin user: %w", err)
	}
	u.Roles = roles
	return domain.AdminUser{User: u, ActiveSessions: sessions, Version: version}, nil
}

func (r *Repository) RecordAdminAction(ctx context.Context, action authz.Action) error {
	if err := outbox.Write(ctx, r.q, OutboxTable, action.Event(uuid.New())); err != nil {
		return fmt.Errorf("repository: record admin action %s: %w", action.Name, err)
	}
	return nil
}

func (r *Repository) lockAdmins(ctx context.Context) error {
	if _, err := r.q.Exec(ctx, `SELECT 1 FROM user_roles WHERE role_id = $1 FOR UPDATE`, domain.RoleAdmin); err != nil {
		return fmt.Errorf("repository: lock administrators: %w", err)
	}
	return nil
}

func (r *Repository) keepsAnAdmin(ctx context.Context, without uuid.UUID) error {
	const query = `
		SELECT
			EXISTS (SELECT 1 FROM user_roles WHERE role_id = $1 AND user_id = $2),
			(SELECT count(*) FROM user_roles ur JOIN users u ON u.id = ur.user_id
				WHERE ur.role_id = $1 AND u.suspended_at IS NULL AND u.id <> $2)`

	var (
		isAdmin bool
		others  int
	)
	if err := r.q.QueryRow(ctx, query, domain.RoleAdmin, without).Scan(&isAdmin, &others); err != nil {
		return fmt.Errorf("repository: count administrators: %w", err)
	}
	if isAdmin && others == 0 {
		return domain.ErrLastAdmin
	}
	return nil
}

func (r *Repository) SuspendUser(ctx context.Context, userID uuid.UUID, reason string, at time.Time, action authz.Action) (domain.User, error) {
	var suspended domain.User
	err := r.InTx(ctx, func(tx *Repository) error {
		if err := tx.lockAdmins(ctx); err != nil {
			return err
		}
		if err := tx.keepsAnAdmin(ctx, userID); err != nil {
			return err
		}
		const query = `
			UPDATE users SET suspended_at = $2, suspended_reason = $3, updated_at = $2
			WHERE id = $1 AND suspended_at IS NULL
			RETURNING ` + userColumns
		user, err := scanUser(tx.q.QueryRow(ctx, query, userID, at, reason))
		if noRows(err) {
			if _, lookup := tx.UserByID(ctx, userID); lookup != nil {
				return lookup
			}
			return domain.ErrAlreadySuspended
		}
		if err != nil {
			return fmt.Errorf("repository: suspend user: %w", err)
		}
		revoked, err := tx.RevokeAllForUser(ctx, userID, at)
		if err != nil {
			return err
		}
		action.Details = map[string]string{"revoked_tokens": strconv.FormatInt(revoked, 10)}
		suspended = user
		return tx.RecordAdminAction(ctx, action)
	})
	return suspended, err
}

func (r *Repository) RestoreUser(ctx context.Context, userID uuid.UUID, at time.Time, action authz.Action) (domain.User, error) {
	var restored domain.User
	err := r.InTx(ctx, func(tx *Repository) error {
		const query = `
			UPDATE users SET suspended_at = NULL, suspended_reason = '', updated_at = $2
			WHERE id = $1 AND suspended_at IS NOT NULL
			RETURNING ` + userColumns
		user, err := scanUser(tx.q.QueryRow(ctx, query, userID, at))
		if noRows(err) {
			if _, lookup := tx.UserByID(ctx, userID); lookup != nil {
				return lookup
			}
			return domain.ErrNotSuspended
		}
		if err != nil {
			return fmt.Errorf("repository: restore user: %w", err)
		}
		restored = user
		return tx.RecordAdminAction(ctx, action)
	})
	return restored, err
}

func (r *Repository) RevokeUserSessions(ctx context.Context, userID uuid.UUID, at time.Time, action authz.Action) (int64, error) {
	var revoked int64
	err := r.InTx(ctx, func(tx *Repository) error {
		if _, err := tx.UserByID(ctx, userID); err != nil {
			return err
		}
		n, err := tx.RevokeAllForUser(ctx, userID, at)
		if err != nil {
			return err
		}
		revoked = n
		action.Details = map[string]string{"revoked_tokens": strconv.FormatInt(n, 10)}
		return tx.RecordAdminAction(ctx, action)
	})
	return revoked, err
}

func (r *Repository) ChangeRole(ctx context.Context, userID, roleID uuid.UUID, grant bool, action authz.Action) error {
	return r.InTx(ctx, func(tx *Repository) error {
		if grant {
			if err := tx.AssignRole(ctx, userID, roleID); err != nil {
				return err
			}
		} else {
			if roleID == domain.RoleAdmin {
				if err := tx.lockAdmins(ctx); err != nil {
					return err
				}
				if err := tx.keepsAnAdmin(ctx, userID); err != nil {
					return err
				}
			}
			if err := tx.RevokeRole(ctx, userID, roleID); err != nil {
				return err
			}
		}
		if err := tx.RecordAdminAction(ctx, action); err != nil {
			return err
		}
		event := domain.SecurityEvent{Action: domain.SecurityRoleRevoked, UserID: userID, ActorID: action.ActorID, Subject: roleID.String(),
			Device: domain.Device{IP: action.ClientIP, UserAgent: action.UserAgent}, OccurredAt: action.At}
		if grant {
			event.Action = domain.SecurityRoleAssigned
		}
		return tx.RecordSecurityEvent(ctx, event)
	})
}
