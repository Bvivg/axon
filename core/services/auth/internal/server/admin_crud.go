package server

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/pkg/admincrud"
	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

var roleFields = admincrud.Fields{
	"title":       authz.RolesMutate,
	"permissions": authz.RolesMutate,
}

func (a *Admin) actorWithPermissions(ctx context.Context) (uuid.UUID, authz.Set, error) {
	actor, ok := authz.ActorFrom(ctx)
	if !ok {
		return uuid.Nil, nil, connect.NewError(connect.CodeUnauthenticated, errors.New("no access token"))
	}
	return actor.UserID, actor.Permissions, nil
}

func (a *Admin) fail(ctx context.Context, err error) error {
	return translateError(ctx, a.Handler.log, err)
}

func parseID(field, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, admincrud.Invalid(field, "uuid", field+" is not valid")
	}
	return id, nil
}

func toProtoAdminRole(r domain.AdminRole) *authv1.AdminRole {
	return &authv1.AdminRole{
		Id:          r.ID.String(),
		Name:        r.Name,
		Title:       r.Title,
		Permissions: r.Permissions,
		System:      r.System,
		MemberCount: r.Members,
		Etag:        admincrud.Etag(r.Version),
		CreateTime:  timestamppb.New(r.CreatedAt),
		UpdateTime:  timestamppb.New(r.UpdatedAt),
	}
}

func (a *Admin) ListPermissions(ctx context.Context, _ *connect.Request[authv1.ListPermissionsRequest]) (*connect.Response[authv1.ListPermissionsResponse], error) {
	catalog, err := a.Handler.svc.PermissionCatalog(ctx)
	if err != nil {
		return nil, a.fail(ctx, err)
	}
	out := &authv1.ListPermissionsResponse{Permissions: make([]*authv1.PermissionInfo, len(catalog))}
	for i, p := range catalog {
		out.Permissions[i] = &authv1.PermissionInfo{Name: p.Namespace + ":" + p.Action, Namespace: p.Namespace, Action: p.Action}
	}
	return connect.NewResponse(out), nil
}

func (a *Admin) ListAdminRoles(ctx context.Context, _ *connect.Request[authv1.ListAdminRolesRequest]) (*connect.Response[authv1.ListAdminRolesResponse], error) {
	roles, err := a.Handler.svc.AdminRoles(ctx)
	if err != nil {
		return nil, a.fail(ctx, err)
	}
	out := &authv1.ListAdminRolesResponse{Roles: make([]*authv1.AdminRole, len(roles))}
	for i, role := range roles {
		out.Roles[i] = toProtoAdminRole(role)
	}
	return connect.NewResponse(out), nil
}

func (a *Admin) GetRole(ctx context.Context, req *connect.Request[authv1.GetRoleRequest]) (*connect.Response[authv1.GetRoleResponse], error) {
	id, err := parseID("role_id", req.Msg.GetRoleId())
	if err != nil {
		return nil, err
	}
	role, err := a.Handler.svc.AdminRole(ctx, id)
	if err != nil {
		return nil, a.fail(ctx, err)
	}
	return connect.NewResponse(&authv1.GetRoleResponse{Role: toProtoAdminRole(role)}), nil
}

func fromProtoRole(r *authv1.AdminRole) domain.AdminRole {
	return domain.AdminRole{Name: r.GetName(), Title: r.GetTitle(), Permissions: r.GetPermissions()}
}

func (a *Admin) CreateRole(ctx context.Context, req *connect.Request[authv1.CreateRoleRequest]) (*connect.Response[authv1.CreateRoleResponse], error) {
	actorID, err := a.actor(ctx)
	if err != nil {
		return nil, err
	}
	requestID, err := parseID("request_id", req.Msg.GetRequestId())
	if err != nil {
		return nil, err
	}
	reason, err := authz.Reason(req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	role, err := a.Handler.svc.CreateRole(ctx, actorID, requestID, fromProtoRole(req.Msg.GetRole()), reason, req.Msg.GetValidateOnly())
	if err != nil {
		return nil, a.fail(ctx, err)
	}
	return connect.NewResponse(&authv1.CreateRoleResponse{Role: toProtoAdminRole(role)}), nil
}

func (a *Admin) UpdateRole(ctx context.Context, req *connect.Request[authv1.UpdateRoleRequest]) (*connect.Response[authv1.UpdateRoleResponse], error) {
	actorID, granted, err := a.actorWithPermissions(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID("role.id", req.Msg.GetRole().GetId())
	if err != nil {
		return nil, err
	}
	version, err := admincrud.ParseEtag(req.Msg.GetRole().GetEtag())
	if err != nil {
		return nil, err
	}
	paths, err := roleFields.Paths(req.Msg.GetUpdateMask(), granted)
	if err != nil {
		return nil, err
	}
	reason, err := authz.Reason(req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	fields := domain.RoleFields{}
	for _, path := range paths {
		switch path {
		case "title":
			fields.Title = true
		case "permissions":
			fields.Permissions = true
		}
	}
	role, err := a.Handler.svc.UpdateRole(ctx, actorID, id, version, fields, fromProtoRole(req.Msg.GetRole()), reason, req.Msg.GetValidateOnly())
	if err != nil {
		return nil, a.fail(ctx, err)
	}
	return connect.NewResponse(&authv1.UpdateRoleResponse{Role: toProtoAdminRole(role)}), nil
}

func (a *Admin) DeleteRole(ctx context.Context, req *connect.Request[authv1.DeleteRoleRequest]) (*connect.Response[authv1.DeleteRoleResponse], error) {
	actorID, err := a.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID("role_id", req.Msg.GetRoleId())
	if err != nil {
		return nil, err
	}
	version, err := admincrud.ParseEtag(req.Msg.GetEtag())
	if err != nil {
		return nil, err
	}
	reason, err := authz.Reason(req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	if err := a.Handler.svc.DeleteRole(ctx, actorID, id, version, reason); err != nil {
		return nil, a.fail(ctx, err)
	}
	return connect.NewResponse(&authv1.DeleteRoleResponse{}), nil
}

func (a *Admin) UpdateUserProfile(ctx context.Context, req *connect.Request[authv1.UpdateUserProfileRequest]) (*connect.Response[authv1.UpdateUserProfileResponse], error) {
	actorID, userID, reason, err := a.target(ctx, req.Msg.GetUserId(), req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	version, err := admincrud.ParseEtag(req.Msg.GetEtag())
	if err != nil {
		return nil, err
	}
	user, err := a.Handler.svc.UpdateUserDisplayName(ctx, actorID, userID, version, req.Msg.GetDisplayName(), reason)
	if err != nil {
		return nil, a.fail(ctx, err)
	}
	return connect.NewResponse(&authv1.UpdateUserProfileResponse{User: a.toProtoAdminUser(user)}), nil
}

func (a *Admin) SetUserRoles(ctx context.Context, req *connect.Request[authv1.SetUserRolesRequest]) (*connect.Response[authv1.SetUserRolesResponse], error) {
	actorID, userID, reason, err := a.target(ctx, req.Msg.GetUserId(), req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	user, err := a.Handler.svc.SetUserRoles(ctx, actorID, userID, req.Msg.GetRoles(), reason)
	if err != nil {
		return nil, a.fail(ctx, err)
	}
	return connect.NewResponse(&authv1.SetUserRolesResponse{User: a.toProtoAdminUser(user)}), nil
}

func toProtoLinked(accounts []domain.LinkedAccount) []*authv1.LinkedAccount {
	out := make([]*authv1.LinkedAccount, len(accounts))
	for i, account := range accounts {
		out[i] = &authv1.LinkedAccount{Provider: string(account.Provider), Email: account.Email, LinkedAt: timestamppb.New(account.LinkedAt)}
	}
	return out
}

func (a *Admin) UnlinkOAuthAccount(ctx context.Context, req *connect.Request[authv1.UnlinkOAuthAccountRequest]) (*connect.Response[authv1.UnlinkOAuthAccountResponse], error) {
	actorID, userID, reason, err := a.target(ctx, req.Msg.GetUserId(), req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	accounts, err := a.Handler.svc.UnlinkOAuthAccount(ctx, actorID, userID, domain.Provider(req.Msg.GetProvider()), reason)
	if err != nil {
		return nil, a.fail(ctx, err)
	}
	return connect.NewResponse(&authv1.UnlinkOAuthAccountResponse{LinkedAccounts: toProtoLinked(accounts)}), nil
}

var exportHeader = []string{"occurred_at", "service", "action", "outcome", "actor_id", "target_type", "target_id", "reason", "changes", "details", "correlation_id", "client_ip", "user_agent"}

func cell(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}
	return value
}

func joinChanges(changes []domain.AuditChange) string {
	parts := make([]string, len(changes))
	for i, c := range changes {
		parts[i] = c.Field + ": " + c.Before + " → " + c.After
	}
	return strings.Join(parts, "; ")
}

func joinDetails(details map[string]string) string {
	parts := make([]string, 0, len(details))
	for key, value := range details {
		parts = append(parts, key+"="+value)
	}
	slices.Sort(parts)
	return strings.Join(parts, "; ")
}

func (a *Admin) ExportAuditEvents(ctx context.Context, req *connect.Request[authv1.ExportAuditEventsRequest]) (*connect.Response[authv1.ExportAuditEventsResponse], error) {
	actorID, err := a.actor(ctx)
	if err != nil {
		return nil, err
	}
	filter, err := auditFilter(req.Msg.GetFilter())
	if err != nil {
		return nil, err
	}
	events, truncated, err := a.Handler.svc.ExportAuditEvents(ctx, actorID, filter)
	if err != nil {
		return nil, a.fail(ctx, admincrud.QueryError(err))
	}
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write(exportHeader); err != nil {
		return nil, a.fail(ctx, err)
	}
	for _, e := range events {
		actor := ""
		if e.ActorID != nil {
			actor = e.ActorID.String()
		}
		record := []string{e.OccurredAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"), e.Service, e.Action, string(e.Outcome), actor, e.TargetType, e.TargetID,
			e.Reason, joinChanges(e.Changes), joinDetails(e.Details), e.CorrelationID, e.ClientIP, e.UserAgent}
		for i := range record {
			record[i] = cell(record[i])
		}
		if err := writer.Write(record); err != nil {
			return nil, a.fail(ctx, err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, a.fail(ctx, err)
	}
	return connect.NewResponse(&authv1.ExportAuditEventsResponse{Csv: buf.Bytes(), Rows: int32(len(events)), Truncated: truncated}), nil
}
