package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

var errDryRun = errors.New("repository: dry run")

const adminRoleQuery = `
	SELECT r.id, r.name, r.title, r.system, r.version, r.created_at, r.updated_at,
		ARRAY(SELECT p.namespace || ':' || p.action FROM role_permissions rp JOIN permissions p ON p.id = rp.permission_id
			WHERE rp.role_id = r.id ORDER BY 1),
		(SELECT count(*) FROM user_roles ur WHERE ur.role_id = r.id)
	FROM roles r`

func scanAdminRole(row scanRow) (domain.AdminRole, error) {
	var role domain.AdminRole
	err := row.Scan(&role.ID, &role.Name, &role.Title, &role.System, &role.Version, &role.CreatedAt, &role.UpdatedAt,
		&role.Permissions, &role.Members)
	if role.Permissions == nil {
		role.Permissions = []string{}
	}
	return role, err
}

func (r *Repository) PermissionCatalog(ctx context.Context) ([]domain.Permission, error) {
	rows, err := r.q.Query(ctx, `SELECT id, namespace, action, parent_id FROM permissions ORDER BY namespace, action`)
	if err != nil {
		return nil, fmt.Errorf("repository: permission catalog: %w", err)
	}
	defer rows.Close()
	out := []domain.Permission{}
	for rows.Next() {
		var p domain.Permission
		if err := rows.Scan(&p.ID, &p.Namespace, &p.Action, &p.ParentID); err != nil {
			return nil, fmt.Errorf("repository: scan permission: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: permission catalog: %w", err)
	}
	return out, nil
}

func (r *Repository) AdminRoles(ctx context.Context) ([]domain.AdminRole, error) {
	rows, err := r.q.Query(ctx, adminRoleQuery+` ORDER BY r.system DESC, r.name`)
	if err != nil {
		return nil, fmt.Errorf("repository: admin roles: %w", err)
	}
	defer rows.Close()
	out := []domain.AdminRole{}
	for rows.Next() {
		role, err := scanAdminRole(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan admin role: %w", err)
		}
		out = append(out, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: admin roles: %w", err)
	}
	return out, nil
}

func (r *Repository) AdminRole(ctx context.Context, id uuid.UUID) (domain.AdminRole, error) {
	role, err := scanAdminRole(r.q.QueryRow(ctx, adminRoleQuery+` WHERE r.id = $1`, id))
	if noRows(err) {
		return domain.AdminRole{}, domain.ErrRoleNotFound
	}
	if err != nil {
		return domain.AdminRole{}, fmt.Errorf("repository: admin role: %w", err)
	}
	return role, nil
}

func (r *Repository) permissionIDs(ctx context.Context, names []string) ([]uuid.UUID, error) {
	if len(names) == 0 {
		return nil, nil
	}
	rows, err := r.q.Query(ctx, `SELECT id, namespace || ':' || action FROM permissions WHERE namespace || ':' || action = ANY($1)`, names)
	if err != nil {
		return nil, fmt.Errorf("repository: resolve permissions: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	found := map[string]bool{}
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("repository: scan permission id: %w", err)
		}
		ids = append(ids, id)
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: resolve permissions: %w", err)
	}
	for _, name := range names {
		if !found[name] {
			return nil, fmt.Errorf("%w: %s", domain.ErrUnknownPermission, name)
		}
	}
	return ids, nil
}

func (r *Repository) grant(ctx context.Context, roleID uuid.UUID, permissions []uuid.UUID) error {
	if len(permissions) == 0 {
		return nil
	}
	if _, err := r.q.Exec(ctx, `INSERT INTO role_permissions (role_id, permission_id) SELECT $1, unnest($2::uuid[])`, roleID, permissions); err != nil {
		return fmt.Errorf("repository: grant permissions: %w", err)
	}
	return nil
}

func (r *Repository) inTxDry(ctx context.Context, dryRun bool, fn func(*Repository) error) error {
	err := r.InTx(ctx, func(tx *Repository) error {
		if err := fn(tx); err != nil {
			return err
		}
		if dryRun {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		return nil
	}
	return err
}

func (r *Repository) CreateRole(ctx context.Context, role domain.AdminRole, at time.Time, action authz.Action, dryRun bool) (domain.AdminRole, error) {
	var created domain.AdminRole
	err := r.inTxDry(ctx, dryRun, func(tx *Repository) error {
		existing, err := tx.AdminRole(ctx, role.ID)
		switch {
		case err == nil && existing.Name == role.Name && !existing.System:
			created = existing
			return errDryRun
		case err == nil:
			return domain.ErrRequestReused
		case !errors.Is(err, domain.ErrRoleNotFound):
			return err
		}
		ids, err := tx.permissionIDs(ctx, role.Permissions)
		if err != nil {
			return err
		}
		if _, err := tx.q.Exec(ctx, `INSERT INTO roles (id, name, title, system, version, created_at, updated_at) VALUES ($1, $2, $3, false, 1, $4, $4)`,
			role.ID, role.Name, role.Title, at); err != nil {
			if isUniqueViolation(err, "roles_name_key") {
				return domain.ErrRoleExists
			}
			return fmt.Errorf("repository: create role: %w", err)
		}
		if err := tx.grant(ctx, role.ID, ids); err != nil {
			return err
		}
		if created, err = tx.AdminRole(ctx, role.ID); err != nil {
			return err
		}
		return tx.RecordAdminAction(ctx, action)
	})
	return created, err
}

func (r *Repository) lockRole(ctx context.Context, id uuid.UUID) (domain.AdminRole, error) {
	if _, err := r.q.Exec(ctx, `SELECT 1 FROM roles WHERE id = $1 FOR UPDATE`, id); err != nil {
		return domain.AdminRole{}, fmt.Errorf("repository: lock role: %w", err)
	}
	return r.AdminRole(ctx, id)
}

func sameRole(current, next domain.AdminRole, fields domain.RoleFields) bool {
	if fields.Title && current.Title != next.Title {
		return false
	}
	if fields.Permissions {
		want := slices.Clone(next.Permissions)
		slices.Sort(want)
		if !slices.Equal(current.Permissions, want) {
			return false
		}
	}
	return true
}

func (r *Repository) UpdateRole(ctx context.Context, id uuid.UUID, version int64, fields domain.RoleFields, next domain.AdminRole, at time.Time, action authz.Action, diff func(before, after domain.AdminRole) []authz.Change, dryRun bool) (domain.AdminRole, error) {
	var updated domain.AdminRole
	err := r.inTxDry(ctx, dryRun, func(tx *Repository) error {
		before, err := tx.lockRole(ctx, id)
		if err != nil {
			return err
		}
		if before.System {
			return domain.ErrSystemRole
		}
		if before.Version != version {
			if sameRole(before, next, fields) {
				updated = before
				return errDryRun
			}
			return &domain.VersionConflict{Current: before.Version}
		}
		if fields.Title {
			if _, err := tx.q.Exec(ctx, `UPDATE roles SET title = $2 WHERE id = $1`, id, next.Title); err != nil {
				return fmt.Errorf("repository: update role title: %w", err)
			}
		}
		if fields.Permissions {
			ids, err := tx.permissionIDs(ctx, next.Permissions)
			if err != nil {
				return err
			}
			if _, err := tx.q.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, id); err != nil {
				return fmt.Errorf("repository: clear role permissions: %w", err)
			}
			if err := tx.grant(ctx, id, ids); err != nil {
				return err
			}
		}
		if _, err := tx.q.Exec(ctx, `UPDATE roles SET version = version + 1, updated_at = $2 WHERE id = $1`, id, at); err != nil {
			return fmt.Errorf("repository: bump role version: %w", err)
		}
		if updated, err = tx.AdminRole(ctx, id); err != nil {
			return err
		}
		action.Changes = diff(before, updated)
		return tx.RecordAdminAction(ctx, action)
	})
	return updated, err
}

type deletedRole struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Permissions []string `json:"permissions"`
	Version     int64    `json:"version"`
}

func (r *Repository) DeleteRole(ctx context.Context, id uuid.UUID, version int64, at time.Time, action authz.Action, diff func(before, after domain.AdminRole) []authz.Change) error {
	return r.InTx(ctx, func(tx *Repository) error {
		before, err := tx.lockRole(ctx, id)
		if err != nil {
			return err
		}
		switch {
		case before.System:
			return domain.ErrSystemRole
		case before.Version != version:
			return &domain.VersionConflict{Current: before.Version}
		case before.Members > 0:
			return domain.ErrRoleInUse
		}
		record, err := json.Marshal(deletedRole{Name: before.Name, Title: before.Title, Permissions: before.Permissions, Version: before.Version})
		if err != nil {
			return fmt.Errorf("repository: encode deleted role: %w", err)
		}
		if _, err := tx.q.Exec(ctx, `INSERT INTO deleted_records (id, kind, record_id, record, deleted_by, deleted_at) VALUES ($1, 'role', $2, $3, $4, $5)`,
			uuid.New(), id, record, nullableActor(action.ActorID), at); err != nil {
			return fmt.Errorf("repository: keep deleted role: %w", err)
		}
		if _, err := tx.q.Exec(ctx, `DELETE FROM roles WHERE id = $1`, id); err != nil {
			return fmt.Errorf("repository: delete role: %w", err)
		}
		action.Changes = diff(before, domain.AdminRole{})
		return tx.RecordAdminAction(ctx, action)
	})
}

func nullableActor(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func (r *Repository) SetUserRoles(ctx context.Context, userID uuid.UUID, names []string, action authz.Action) error {
	return r.InTx(ctx, func(tx *Repository) error {
		if _, err := tx.q.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
			return fmt.Errorf("repository: lock user: %w", err)
		}
		if _, err := tx.UserByID(ctx, userID); err != nil {
			return err
		}
		before, err := tx.RoleNamesForUser(ctx, userID)
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(names))
		for _, name := range names {
			var id uuid.UUID
			if err := tx.q.QueryRow(ctx, `SELECT id FROM roles WHERE name = $1`, name).Scan(&id); err != nil {
				if noRows(err) {
					return fmt.Errorf("%w: %s", domain.ErrRoleNotFound, name)
				}
				return fmt.Errorf("repository: resolve role: %w", err)
			}
			ids = append(ids, id)
		}
		if slices.Contains(before, "admin") && !slices.Contains(names, "admin") {
			if err := tx.lockAdmins(ctx); err != nil {
				return err
			}
			if err := tx.keepsAnAdmin(ctx, userID); err != nil {
				return err
			}
		}
		if _, err := tx.q.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1 AND NOT (role_id = ANY($2::uuid[]))`, userID, ids); err != nil {
			return fmt.Errorf("repository: drop user roles: %w", err)
		}
		if _, err := tx.q.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING`, userID, ids); err != nil {
			return fmt.Errorf("repository: add user roles: %w", err)
		}
		after := slices.Clone(names)
		slices.Sort(after)
		action.Changes = []authz.Change{{Field: "roles", Before: joinNames(before), After: joinNames(after)}}
		if err := tx.RecordAdminAction(ctx, action); err != nil {
			return err
		}
		for _, name := range after {
			if !slices.Contains(before, name) {
				if err := tx.RecordSecurityEvent(ctx, roleEvent(domain.SecurityRoleAssigned, userID, name, action)); err != nil {
					return err
				}
			}
		}
		for _, name := range before {
			if !slices.Contains(after, name) {
				if err := tx.RecordSecurityEvent(ctx, roleEvent(domain.SecurityRoleRevoked, userID, name, action)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func roleEvent(kind domain.SecurityAction, userID uuid.UUID, role string, action authz.Action) domain.SecurityEvent {
	return domain.SecurityEvent{Action: kind, UserID: userID, ActorID: action.ActorID, Subject: role,
		Device: domain.Device{IP: action.ClientIP, UserAgent: action.UserAgent}, OccurredAt: action.At}
}

func joinNames(names []string) string {
	out := ""
	for i, name := range names {
		if i > 0 {
			out += ", "
		}
		out += name
	}
	return out
}

func (r *Repository) UpdateUserDisplayName(ctx context.Context, userID uuid.UUID, version int64, name string, at time.Time, action authz.Action) error {
	return r.InTx(ctx, func(tx *Repository) error {
		var (
			current string
			stored  int64
		)
		err := tx.q.QueryRow(ctx, `SELECT display_name, version FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&current, &stored)
		if noRows(err) {
			return domain.ErrUserNotFound
		}
		if err != nil {
			return fmt.Errorf("repository: lock user profile: %w", err)
		}
		if stored != version {
			if current == name {
				return nil
			}
			return &domain.VersionConflict{Current: stored}
		}
		if current == name {
			return nil
		}
		if _, err := tx.q.Exec(ctx, `UPDATE users SET display_name = $2, updated_at = $3 WHERE id = $1`, userID, name, at); err != nil {
			return fmt.Errorf("repository: update display name: %w", err)
		}
		action.Changes = []authz.Change{{Field: "display_name", Before: current, After: name}}
		return tx.RecordAdminAction(ctx, action)
	})
}

func (r *Repository) LinkedAccounts(ctx context.Context, userID uuid.UUID) ([]domain.LinkedAccount, bool, error) {
	accounts, err := r.OauthAccountsForUser(ctx, userID)
	if err != nil {
		return nil, false, err
	}
	var password bool
	if err := r.q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM credentials WHERE user_id = $1)`, userID).Scan(&password); err != nil {
		return nil, false, fmt.Errorf("repository: has password: %w", err)
	}
	out := make([]domain.LinkedAccount, len(accounts))
	for i, a := range accounts {
		out[i] = domain.LinkedAccount{Provider: a.Provider, Email: a.Email, LinkedAt: a.LinkedAt}
	}
	return out, password, nil
}

func (r *Repository) UnlinkOAuthAccount(ctx context.Context, userID uuid.UUID, provider domain.Provider, action authz.Action) error {
	return r.InTx(ctx, func(tx *Repository) error {
		if _, err := tx.q.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
			return fmt.Errorf("repository: lock user: %w", err)
		}
		if _, err := tx.UserByID(ctx, userID); err != nil {
			return err
		}
		accounts, password, err := tx.LinkedAccounts(ctx, userID)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(accounts, func(a domain.LinkedAccount) bool { return a.Provider == provider }) {
			return domain.ErrLinkedAccountNotFound
		}
		if !password && len(accounts) == 1 {
			return domain.ErrOnlyLogin
		}
		if _, err := tx.q.Exec(ctx, `DELETE FROM oauth_accounts WHERE user_id = $1 AND provider = $2`, userID, string(provider)); err != nil {
			return fmt.Errorf("repository: unlink oauth account: %w", err)
		}
		action.Details = map[string]string{"provider": string(provider)}
		return tx.RecordAdminAction(ctx, action)
	})
}
