package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func (r *Repository) ListRoles(ctx context.Context) ([]domain.Role, error) {
	const query = `
		SELECT r.id, r.name, p.id, p.namespace, p.action, p.parent_id
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		LEFT JOIN permissions p ON p.id = rp.permission_id
		ORDER BY r.name, p.namespace, p.action`

	rows, err := r.q.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("repository: list roles: %w", err)
	}
	defer rows.Close()

	order := make([]uuid.UUID, 0)
	byID := make(map[uuid.UUID]*domain.Role)

	for rows.Next() {
		var (
			roleID   uuid.UUID
			roleName string

			permID        *uuid.UUID
			permNamespace *string
			permAction    *string
			permParentID  *uuid.UUID
		)
		if err := rows.Scan(&roleID, &roleName, &permID, &permNamespace, &permAction, &permParentID); err != nil {
			return nil, fmt.Errorf("repository: scan role: %w", err)
		}

		role, ok := byID[roleID]
		if !ok {
			role = &domain.Role{ID: roleID, Name: roleName}
			byID[roleID] = role
			order = append(order, roleID)
		}

		if permID != nil {
			role.Permissions = append(role.Permissions, domain.Permission{
				ID:        *permID,
				Namespace: *permNamespace,
				Action:    *permAction,
				ParentID:  permParentID,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list roles: %w", err)
	}

	out := make([]domain.Role, len(order))
	for i, id := range order {
		out[i] = *byID[id]
	}
	return out, nil
}

func (r *Repository) RoleNamesForUser(ctx context.Context, userID uuid.UUID) ([]string, error) {
	const query = `
		SELECT r.name
		FROM roles r
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = $1
		ORDER BY r.name`

	rows, err := r.q.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: role names for user: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("repository: scan role name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: role names for user: %w", err)
	}
	return names, nil
}

func (r *Repository) AssignRole(ctx context.Context, userID, roleID uuid.UUID) error {
	const query = `
		INSERT INTO user_roles (user_id, role_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id, role_id) DO NOTHING`

	if _, err := r.q.Exec(ctx, query, userID, roleID); err != nil {
		if isForeignKeyViolation(err, "user_roles_role_id_fkey") {
			return domain.ErrRoleNotFound
		}
		if isForeignKeyViolation(err, "user_roles_user_id_fkey") {
			return domain.ErrUserNotFound
		}
		return fmt.Errorf("repository: assign role: %w", err)
	}
	return nil
}

func (r *Repository) RevokeRole(ctx context.Context, userID, roleID uuid.UUID) error {
	const query = `DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2`

	tag, err := r.q.Exec(ctx, query, userID, roleID)
	if err != nil {
		return fmt.Errorf("repository: revoke role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRoleNotFound
	}
	return nil
}

func (r *Repository) UserHasPermission(ctx context.Context, userID uuid.UUID, namespace, action string) (bool, error) {
	const query = `
		WITH RECURSIVE ancestors AS (
			SELECT id, parent_id FROM permissions WHERE namespace = $2 AND action = $3
			UNION ALL
			SELECT p.id, p.parent_id FROM permissions p JOIN ancestors a ON p.id = a.parent_id
		)
		SELECT EXISTS (
			SELECT 1
			FROM user_roles ur
			JOIN role_permissions rp ON rp.role_id = ur.role_id
			JOIN ancestors a ON a.id = rp.permission_id
			WHERE ur.user_id = $1
		)`

	var ok bool
	if err := r.q.QueryRow(ctx, query, userID, namespace, action).Scan(&ok); err != nil {
		return false, fmt.Errorf("repository: user has permission: %w", err)
	}
	return ok, nil
}
