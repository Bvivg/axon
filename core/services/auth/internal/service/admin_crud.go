package service

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const (
	auditRole        = "role"
	auditLog         = "audit"
	actionRoleNew    = "role_created"
	actionRoleEdit   = "role_updated"
	actionRoleDelete = "role_deleted"
	actionRolesSet   = "user_roles_changed"
	actionProfile    = "user_profile_changed"
	actionUnlink     = "oauth_account_unlinked"
	actionExport     = "audit_exported"
)

func (s *Service) PermissionCatalog(ctx context.Context) ([]domain.Permission, error) {
	return s.store.PermissionCatalog(ctx)
}

func (s *Service) AdminRoles(ctx context.Context) ([]domain.AdminRole, error) {
	return s.store.AdminRoles(ctx)
}

func (s *Service) AdminRole(ctx context.Context, id uuid.UUID) (domain.AdminRole, error) {
	return s.store.AdminRole(ctx, id)
}

func (s *Service) action(ctx context.Context, actorID uuid.UUID, name, targetType, targetID, reason string) authz.Action {
	action := authz.NewAction(ctx, auditService, name, targetType, targetID, reason)
	action.ActorID = actorID
	action.At = s.now().UTC()
	if action.ClientIP == "" && action.UserAgent == "" {
		device := domain.DeviceFromContext(ctx)
		action.ClientIP, action.UserAgent = device.IP, device.UserAgent
	}
	return action
}

func roleChanges(before, after domain.AdminRole) []authz.Change {
	var out []authz.Change
	add := func(field, was, now string) {
		if was != now {
			out = append(out, authz.Change{Field: field, Before: was, After: now})
		}
	}
	add("name", before.Name, after.Name)
	add("title", before.Title, after.Title)
	add("permissions", strings.Join(sortedCopy(before.Permissions), ", "), strings.Join(sortedCopy(after.Permissions), ", "))
	return out
}

func sortedCopy(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return out
}

func normalizeRole(role domain.AdminRole) domain.AdminRole {
	role.Name = strings.TrimSpace(role.Name)
	role.Title = strings.TrimSpace(role.Title)
	role.Permissions = sortedCopy(role.Permissions)
	role.Permissions = slices.Compact(role.Permissions)
	return role
}

func (s *Service) CreateRole(ctx context.Context, actorID, requestID uuid.UUID, role domain.AdminRole, reason string, dryRun bool) (domain.AdminRole, error) {
	role = normalizeRole(role)
	role.ID = requestID
	action := s.action(ctx, actorID, actionRoleNew, auditRole, requestID.String(), reason)
	action.Changes = roleChanges(domain.AdminRole{}, role)
	created, err := s.store.CreateRole(ctx, role, s.now().UTC(), action, dryRun)
	if err != nil {
		return domain.AdminRole{}, err
	}
	if dryRun {
		return role, nil
	}
	s.log.InfoContext(ctx, "role created", "role_id", created.ID, "actor_id", actorID)
	return created, nil
}

func (s *Service) UpdateRole(ctx context.Context, actorID, id uuid.UUID, version int64, fields domain.RoleFields, next domain.AdminRole, reason string, dryRun bool) (domain.AdminRole, error) {
	next = normalizeRole(next)
	action := s.action(ctx, actorID, actionRoleEdit, auditRole, id.String(), reason)
	updated, err := s.store.UpdateRole(ctx, id, version, fields, next, s.now().UTC(), action, roleChanges, dryRun)
	if err != nil {
		return domain.AdminRole{}, err
	}
	s.log.InfoContext(ctx, "role updated", "role_id", id, "actor_id", actorID, "dry_run", dryRun)
	return updated, nil
}

func (s *Service) DeleteRole(ctx context.Context, actorID, id uuid.UUID, version int64, reason string) error {
	action := s.action(ctx, actorID, actionRoleDelete, auditRole, id.String(), reason)
	if err := s.store.DeleteRole(ctx, id, version, s.now().UTC(), action, roleChanges); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "role deleted", "role_id", id, "actor_id", actorID)
	return nil
}

func (s *Service) SetUserRoles(ctx context.Context, actorID, userID uuid.UUID, roles []string, reason string) (domain.AdminUser, error) {
	if actorID == userID {
		return domain.AdminUser{}, domain.ErrSelfAction
	}
	roles = slices.Compact(sortedCopy(roles))
	action := s.action(ctx, actorID, actionRolesSet, auditUser, userID.String(), reason)
	if err := s.store.SetUserRoles(ctx, userID, roles, action); err != nil {
		return domain.AdminUser{}, err
	}
	s.log.InfoContext(ctx, "user roles changed", "user_id", userID, "actor_id", actorID)
	s.publishUserRevocation(ctx, userID)
	return s.store.AdminUser(ctx, userID, s.now().UTC())
}

func (s *Service) UpdateUserDisplayName(ctx context.Context, actorID, userID uuid.UUID, version int64, name, reason string) (domain.AdminUser, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.AdminUser{}, &domain.ValidationError{Field: "display_name", Reason: "must not be empty"}
	}
	now := s.now().UTC()
	action := s.action(ctx, actorID, actionProfile, auditUser, userID.String(), reason)
	if err := s.store.UpdateUserDisplayName(ctx, userID, version, name, now, action); err != nil {
		return domain.AdminUser{}, err
	}
	s.log.InfoContext(ctx, "user profile changed by an administrator", "user_id", userID, "actor_id", actorID)
	return s.store.AdminUser(ctx, userID, now)
}

func (s *Service) LinkedAccounts(ctx context.Context, userID uuid.UUID) ([]domain.LinkedAccount, bool, error) {
	return s.store.LinkedAccounts(ctx, userID)
}

func (s *Service) UnlinkOAuthAccount(ctx context.Context, actorID, userID uuid.UUID, provider domain.Provider, reason string) ([]domain.LinkedAccount, error) {
	action := s.action(ctx, actorID, actionUnlink, auditUser, userID.String(), reason)
	if err := s.store.UnlinkOAuthAccount(ctx, userID, provider, action); err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "oauth account unlinked", "user_id", userID, "provider", provider, "actor_id", actorID)
	accounts, _, err := s.store.LinkedAccounts(ctx, userID)
	return accounts, err
}

func (s *Service) ExportAuditEvents(ctx context.Context, actorID uuid.UUID, f domain.AuditFilter) ([]domain.AuditEvent, bool, error) {
	events, err := s.store.ExportAuditEvents(ctx, f, domain.MaxAuditExport+1)
	if err != nil {
		return nil, false, err
	}
	truncated := len(events) > domain.MaxAuditExport
	if truncated {
		events = events[:domain.MaxAuditExport]
	}
	action := s.action(ctx, actorID, actionExport, auditLog, "", "")
	action.Details = exportDetails(f, len(events), truncated)
	if err := s.store.RecordAdminAction(ctx, action); err != nil {
		return nil, false, fmt.Errorf("auth: record an audit export: %w", err)
	}
	return events, truncated, nil
}

func exportDetails(f domain.AuditFilter, rows int, truncated bool) map[string]string {
	details := map[string]string{"rows": strconv.Itoa(rows), "truncated": strconv.FormatBool(truncated)}
	if f.ActorID != nil {
		details["actor_id"] = f.ActorID.String()
	}
	for key, value := range map[string]string{"target_type": f.TargetType, "target_id": f.TargetID, "service": f.Service, "action": f.Action, "outcome": string(f.Outcome)} {
		if value != "" {
			details[key] = value
		}
	}
	if f.Since != nil {
		details["since"] = f.Since.Format("2006-01-02T15:04:05Z07:00")
	}
	if f.Until != nil {
		details["until"] = f.Until.Format("2006-01-02T15:04:05Z07:00")
	}
	return details
}
