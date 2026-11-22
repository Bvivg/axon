package server

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/admincrud"
	"github.com/bvivg/axon/core/shared/pkg/adminquery"
	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

type Admin struct {
	Handler *Handler
}

var _ authv1connect.AuthAdminServiceHandler = (*Admin)(nil)

var AdminRules = authz.Rules{
	authv1connect.AuthAdminServiceListUsersProcedure:          authz.UsersRead,
	authv1connect.AuthAdminServiceGetUserProcedure:            authz.UsersRead,
	authv1connect.AuthAdminServiceSuspendUserProcedure:        authz.UsersMutate,
	authv1connect.AuthAdminServiceRestoreUserProcedure:        authz.UsersMutate,
	authv1connect.AuthAdminServiceRevokeUserSessionsProcedure: authz.UsersMutate,
	authv1connect.AuthAdminServiceListAuditEventsProcedure:    authz.AuditRead,
	authv1connect.AuthAdminServiceExportAuditEventsProcedure:  authz.AuditRead,
	authv1connect.AuthAdminServiceUpdateUserProfileProcedure:  authz.UsersMutate,
	authv1connect.AuthAdminServiceSetUserRolesProcedure:       authz.RolesMutate,
	authv1connect.AuthAdminServiceUnlinkOAuthAccountProcedure: authz.UsersMutate,
	authv1connect.AuthAdminServiceListPermissionsProcedure:    authz.RolesRead,
	authv1connect.AuthAdminServiceListAdminRolesProcedure:     authz.RolesRead,
	authv1connect.AuthAdminServiceGetRoleProcedure:            authz.RolesRead,
	authv1connect.AuthAdminServiceCreateRoleProcedure:         authz.RolesMutate,
	authv1connect.AuthAdminServiceUpdateRoleProcedure:         authz.RolesMutate,
	authv1connect.AuthAdminServiceDeleteRoleProcedure:         authz.RolesMutate,
}

func (a *Admin) Interceptor(cache *authz.Cache) connect.Interceptor {
	guard := authz.NewInterceptor(authz.Config{
		Service:  "auth",
		Verifier: a.Handler.verifier,
		Cache:    cache,
		Rules:    AdminRules,
		Record:   a.Handler.svc.RecordAdminAction,
		Logger:   a.Handler.log,
	})
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		guarded := guard(next)
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if req.Spec().Procedure == authv1connect.AuthAdminServiceGetMyPermissionsProcedure {
				return next(ctx, req)
			}
			return guarded(ctx, req)
		}
	})
}

func LocalPermissions(h *Handler) authz.Source {
	return authz.SourceFunc(func(ctx context.Context, user uuid.UUID, _ string) ([]string, error) {
		return h.svc.Permissions(ctx, user)
	})
}

func (a *Admin) actor(ctx context.Context) (uuid.UUID, error) {
	if actor, ok := authz.ActorFrom(ctx); ok {
		return actor.UserID, nil
	}
	return uuid.Nil, connect.NewError(connect.CodeUnauthenticated, errors.New("no access token"))
}

func (a *Admin) GetMyPermissions(ctx context.Context, req *connect.Request[authv1.GetMyPermissionsRequest]) (*connect.Response[authv1.GetMyPermissionsResponse], error) {
	claims, err := a.Handler.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, a.Handler.log, err)
	}
	permissions, err := a.Handler.svc.Permissions(ctx, claims.UserID)
	if err != nil {
		return nil, translateError(ctx, a.Handler.log, err)
	}
	return connect.NewResponse(&authv1.GetMyPermissionsResponse{Permissions: permissions}), nil
}

func (a *Admin) ListUsers(ctx context.Context, req *connect.Request[authv1.ListUsersRequest]) (*connect.Response[authv1.ListUsersResponse], error) {
	filter := domain.UserFilter{
		Query:  req.Msg.GetQuery(),
		Role:   req.Msg.GetRole(),
		Cursor: req.Msg.GetCursor(),
		Limit:  int(req.Msg.GetPageSize()),
		List:   adminquery.Request{Filter: req.Msg.GetFilter(), OrderBy: req.Msg.GetOrderBy(), Offset: req.Msg.Offset, PageSize: req.Msg.GetPageSize()},
	}
	switch req.Msg.GetStatus() {
	case authv1.UserStatus_USER_STATUS_ACTIVE:
		filter.Status = domain.UserStatusActive
	case authv1.UserStatus_USER_STATUS_SUSPENDED:
		filter.Status = domain.UserStatusSuspended
	}
	switch req.Msg.GetSort() {
	case authv1.UserSort_USER_SORT_OLDEST:
		filter.Sort = domain.UserSortOldest
	case authv1.UserSort_USER_SORT_EMAIL:
		filter.Sort = domain.UserSortEmail
	}
	page, err := a.Handler.svc.ListUsers(ctx, filter)
	if err != nil {
		return nil, translateError(ctx, a.Handler.log, admincrud.QueryError(err))
	}
	out := &authv1.ListUsersResponse{NextCursor: page.Next, Total: page.Total, Users: make([]*authv1.AdminUser, len(page.Users))}
	for i, user := range page.Users {
		out.Users[i] = a.toProtoAdminUser(user)
	}
	return connect.NewResponse(out), nil
}

func (a *Admin) GetUser(ctx context.Context, req *connect.Request[authv1.GetUserRequest]) (*connect.Response[authv1.GetUserResponse], error) {
	actorID, err := a.actor(ctx)
	if err != nil {
		return nil, err
	}
	userID, err := uuid.Parse(req.Msg.GetUserId())
	if err != nil {
		return nil, translateError(ctx, a.Handler.log, domain.ErrUserNotFound)
	}
	user, sessions, err := a.Handler.svc.AdminUser(ctx, actorID, userID)
	if err != nil {
		return nil, translateError(ctx, a.Handler.log, err)
	}
	accounts, password, err := a.Handler.svc.LinkedAccounts(ctx, userID)
	if err != nil {
		return nil, translateError(ctx, a.Handler.log, err)
	}
	out := &authv1.GetUserResponse{User: a.toProtoAdminUser(user), Sessions: make([]*authv1.Session, len(sessions)), LinkedAccounts: toProtoLinked(accounts), HasPassword: password}
	for i, session := range sessions {
		out.Sessions[i] = toProtoSession(session)
	}
	return connect.NewResponse(out), nil
}

func (a *Admin) target(ctx context.Context, rawUser, rawReason string) (uuid.UUID, uuid.UUID, string, error) {
	actorID, err := a.actor(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, "", err
	}
	userID, err := uuid.Parse(rawUser)
	if err != nil {
		return uuid.Nil, uuid.Nil, "", translateError(ctx, a.Handler.log, domain.ErrUserNotFound)
	}
	reason, err := authz.Reason(rawReason)
	if err != nil {
		return uuid.Nil, uuid.Nil, "", err
	}
	return actorID, userID, reason, nil
}

func (a *Admin) SuspendUser(ctx context.Context, req *connect.Request[authv1.SuspendUserRequest]) (*connect.Response[authv1.SuspendUserResponse], error) {
	actorID, userID, reason, err := a.target(ctx, req.Msg.GetUserId(), req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	user, err := a.Handler.svc.SuspendUser(ctx, actorID, userID, reason)
	if err != nil {
		return nil, translateError(ctx, a.Handler.log, err)
	}
	return connect.NewResponse(&authv1.SuspendUserResponse{User: a.toProtoAdminUser(user)}), nil
}

func (a *Admin) RestoreUser(ctx context.Context, req *connect.Request[authv1.RestoreUserRequest]) (*connect.Response[authv1.RestoreUserResponse], error) {
	actorID, userID, reason, err := a.target(ctx, req.Msg.GetUserId(), req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	user, err := a.Handler.svc.RestoreUser(ctx, actorID, userID, reason)
	if err != nil {
		return nil, translateError(ctx, a.Handler.log, err)
	}
	return connect.NewResponse(&authv1.RestoreUserResponse{User: a.toProtoAdminUser(user)}), nil
}

func (a *Admin) RevokeUserSessions(ctx context.Context, req *connect.Request[authv1.RevokeUserSessionsRequest]) (*connect.Response[authv1.RevokeUserSessionsResponse], error) {
	actorID, userID, reason, err := a.target(ctx, req.Msg.GetUserId(), req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	revoked, err := a.Handler.svc.RevokeUserSessions(ctx, actorID, userID, reason)
	if err != nil {
		return nil, translateError(ctx, a.Handler.log, err)
	}
	return connect.NewResponse(&authv1.RevokeUserSessionsResponse{Revoked: revoked}), nil
}

func auditFilter(msg *authv1.ListAuditEventsRequest) (domain.AuditFilter, error) {
	filter := domain.AuditFilter{
		TargetType: msg.GetTargetType(),
		TargetID:   msg.GetTargetId(),
		Service:    msg.GetService(),
		Action:     msg.GetAction(),
		Cursor:     msg.GetCursor(),
		Limit:      int(msg.GetPageSize()),
		List:       adminquery.Request{Filter: msg.GetFilter(), OrderBy: msg.GetOrderBy(), Offset: msg.Offset, PageSize: msg.GetPageSize()},
	}
	if raw := msg.GetActorId(); raw != "" {
		actor, err := uuid.Parse(raw)
		if err != nil {
			return domain.AuditFilter{}, admincrud.Invalid("actor_id", "uuid", "actor_id is not valid")
		}
		filter.ActorID = &actor
	}
	switch msg.GetOutcome() {
	case authv1.AuditOutcome_AUDIT_OUTCOME_SUCCEEDED:
		filter.Outcome = domain.AuditSucceeded
	case authv1.AuditOutcome_AUDIT_OUTCOME_DENIED:
		filter.Outcome = domain.AuditDenied
	case authv1.AuditOutcome_AUDIT_OUTCOME_FAILED:
		filter.Outcome = domain.AuditFailed
	}
	if msg.Since != nil {
		since := msg.GetSince().AsTime()
		filter.Since = &since
	}
	if msg.Until != nil {
		until := msg.GetUntil().AsTime()
		filter.Until = &until
	}
	return filter, nil
}

func (a *Admin) ListAuditEvents(ctx context.Context, req *connect.Request[authv1.ListAuditEventsRequest]) (*connect.Response[authv1.ListAuditEventsResponse], error) {
	filter, err := auditFilter(req.Msg)
	if err != nil {
		return nil, err
	}
	page, err := a.Handler.svc.ListAuditEvents(ctx, filter)
	if err != nil {
		return nil, translateError(ctx, a.Handler.log, admincrud.QueryError(err))
	}
	out := &authv1.ListAuditEventsResponse{NextCursor: page.Next, Total: page.Total, Events: make([]*authv1.AuditEvent, len(page.Events))}
	for i, event := range page.Events {
		out.Events[i] = toProtoAuditEvent(event)
	}
	return connect.NewResponse(out), nil
}

func (a *Admin) toProtoAdminUser(u domain.AdminUser) *authv1.AdminUser {
	out := &authv1.AdminUser{
		User:            toProtoUser(u.User, a.Handler.avatarURLs),
		Status:          authv1.UserStatus_USER_STATUS_ACTIVE,
		SuspendedReason: u.User.SuspendedReason,
		ActiveSessions:  int32(min(u.ActiveSessions, 1<<20)),
		Etag:            admincrud.Etag(max(u.Version, 1)),
	}
	if u.User.SuspendedAt != nil {
		out.Status = authv1.UserStatus_USER_STATUS_SUSPENDED
		out.SuspendedAt = timestamppb.New(*u.User.SuspendedAt)
	}
	if u.User.LastSeenAt != nil {
		out.LastSeenAt = timestamppb.New(*u.User.LastSeenAt)
	}
	return out
}

func toProtoAuditEvent(e domain.AuditEvent) *authv1.AuditEvent {
	out := &authv1.AuditEvent{
		Id:            e.ID.String(),
		OccurredAt:    timestamppb.New(e.OccurredAt),
		Service:       e.Service,
		Action:        e.Action,
		TargetType:    e.TargetType,
		TargetId:      e.TargetID,
		Reason:        e.Reason,
		CorrelationId: e.CorrelationID,
		ClientIp:      e.ClientIP,
		UserAgent:     e.UserAgent,
		Details:       e.Details,
	}
	for _, c := range e.Changes {
		out.Changes = append(out.Changes, &authv1.AuditChange{Field: c.Field, Before: c.Before, After: c.After})
	}
	if e.ActorID != nil {
		out.ActorId = e.ActorID.String()
	}
	switch e.Outcome {
	case domain.AuditSucceeded:
		out.Outcome = authv1.AuditOutcome_AUDIT_OUTCOME_SUCCEEDED
	case domain.AuditDenied:
		out.Outcome = authv1.AuditOutcome_AUDIT_OUTCOME_DENIED
	case domain.AuditFailed:
		out.Outcome = authv1.AuditOutcome_AUDIT_OUTCOME_FAILED
	}
	return out
}
