package proxy

import (
	"context"

	"connectrpc.com/connect"

	analysisv1 "github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1/analysisv1connect"
	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1/callsv1connect"
	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	ratingv1 "github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1/ratingv1connect"
	tournamentv1 "github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1/tournamentv1connect"
)

type AuthAdmin struct {
	client authv1connect.AuthAdminServiceClient
}

var _ authv1connect.AuthAdminServiceHandler = (*AuthAdmin)(nil)

func NewAuthAdmin(client connect.HTTPClient, url string, opts ...connect.ClientOption) *AuthAdmin {
	return &AuthAdmin{client: authv1connect.NewAuthAdminServiceClient(client, url, opts...)}
}

func (p *AuthAdmin) GetMyPermissions(ctx context.Context, req *connect.Request[authv1.GetMyPermissionsRequest]) (*connect.Response[authv1.GetMyPermissionsResponse], error) {
	return p.client.GetMyPermissions(ctx, forward(ctx, req))
}

func (p *AuthAdmin) ListUsers(ctx context.Context, req *connect.Request[authv1.ListUsersRequest]) (*connect.Response[authv1.ListUsersResponse], error) {
	return p.client.ListUsers(ctx, forward(ctx, req))
}

func (p *AuthAdmin) GetUser(ctx context.Context, req *connect.Request[authv1.GetUserRequest]) (*connect.Response[authv1.GetUserResponse], error) {
	return p.client.GetUser(ctx, forward(ctx, req))
}

func (p *AuthAdmin) SuspendUser(ctx context.Context, req *connect.Request[authv1.SuspendUserRequest]) (*connect.Response[authv1.SuspendUserResponse], error) {
	return p.client.SuspendUser(ctx, forward(ctx, req))
}

func (p *AuthAdmin) RestoreUser(ctx context.Context, req *connect.Request[authv1.RestoreUserRequest]) (*connect.Response[authv1.RestoreUserResponse], error) {
	return p.client.RestoreUser(ctx, forward(ctx, req))
}

func (p *AuthAdmin) RevokeUserSessions(ctx context.Context, req *connect.Request[authv1.RevokeUserSessionsRequest]) (*connect.Response[authv1.RevokeUserSessionsResponse], error) {
	return p.client.RevokeUserSessions(ctx, forward(ctx, req))
}

func (p *AuthAdmin) ListAuditEvents(ctx context.Context, req *connect.Request[authv1.ListAuditEventsRequest]) (*connect.Response[authv1.ListAuditEventsResponse], error) {
	return p.client.ListAuditEvents(ctx, forward(ctx, req))
}

func (p *AuthAdmin) ExportAuditEvents(ctx context.Context, req *connect.Request[authv1.ExportAuditEventsRequest]) (*connect.Response[authv1.ExportAuditEventsResponse], error) {
	return p.client.ExportAuditEvents(ctx, forward(ctx, req))
}

func (p *AuthAdmin) UpdateUserProfile(ctx context.Context, req *connect.Request[authv1.UpdateUserProfileRequest]) (*connect.Response[authv1.UpdateUserProfileResponse], error) {
	return p.client.UpdateUserProfile(ctx, forward(ctx, req))
}

func (p *AuthAdmin) SetUserRoles(ctx context.Context, req *connect.Request[authv1.SetUserRolesRequest]) (*connect.Response[authv1.SetUserRolesResponse], error) {
	return p.client.SetUserRoles(ctx, forward(ctx, req))
}

func (p *AuthAdmin) UnlinkOAuthAccount(ctx context.Context, req *connect.Request[authv1.UnlinkOAuthAccountRequest]) (*connect.Response[authv1.UnlinkOAuthAccountResponse], error) {
	return p.client.UnlinkOAuthAccount(ctx, forward(ctx, req))
}

func (p *AuthAdmin) ListPermissions(ctx context.Context, req *connect.Request[authv1.ListPermissionsRequest]) (*connect.Response[authv1.ListPermissionsResponse], error) {
	return p.client.ListPermissions(ctx, forward(ctx, req))
}

func (p *AuthAdmin) ListAdminRoles(ctx context.Context, req *connect.Request[authv1.ListAdminRolesRequest]) (*connect.Response[authv1.ListAdminRolesResponse], error) {
	return p.client.ListAdminRoles(ctx, forward(ctx, req))
}

func (p *AuthAdmin) GetRole(ctx context.Context, req *connect.Request[authv1.GetRoleRequest]) (*connect.Response[authv1.GetRoleResponse], error) {
	return p.client.GetRole(ctx, forward(ctx, req))
}

func (p *AuthAdmin) CreateRole(ctx context.Context, req *connect.Request[authv1.CreateRoleRequest]) (*connect.Response[authv1.CreateRoleResponse], error) {
	return p.client.CreateRole(ctx, forward(ctx, req))
}

func (p *AuthAdmin) UpdateRole(ctx context.Context, req *connect.Request[authv1.UpdateRoleRequest]) (*connect.Response[authv1.UpdateRoleResponse], error) {
	return p.client.UpdateRole(ctx, forward(ctx, req))
}

func (p *AuthAdmin) DeleteRole(ctx context.Context, req *connect.Request[authv1.DeleteRoleRequest]) (*connect.Response[authv1.DeleteRoleResponse], error) {
	return p.client.DeleteRole(ctx, forward(ctx, req))
}

type GameAdmin struct {
	client gamev1connect.GameAdminServiceClient
}

var _ gamev1connect.GameAdminServiceHandler = (*GameAdmin)(nil)

func NewGameAdmin(client connect.HTTPClient, url string, opts ...connect.ClientOption) *GameAdmin {
	return &GameAdmin{client: gamev1connect.NewGameAdminServiceClient(client, url, opts...)}
}

func (p *GameAdmin) ListGameSessions(ctx context.Context, req *connect.Request[gamev1.ListGameSessionsRequest]) (*connect.Response[gamev1.ListGameSessionsResponse], error) {
	return p.client.ListGameSessions(ctx, forward(ctx, req))
}

func (p *GameAdmin) CancelGameSession(ctx context.Context, req *connect.Request[gamev1.CancelGameSessionRequest]) (*connect.Response[gamev1.CancelGameSessionResponse], error) {
	return p.client.CancelGameSession(ctx, forward(ctx, req))
}

func (p *GameAdmin) GetGameSession(ctx context.Context, req *connect.Request[gamev1.GetGameSessionRequest]) (*connect.Response[gamev1.GetGameSessionResponse], error) {
	return p.client.GetGameSession(ctx, forward(ctx, req))
}

func (p *GameAdmin) ListMatchQueue(ctx context.Context, req *connect.Request[gamev1.ListMatchQueueRequest]) (*connect.Response[gamev1.ListMatchQueueResponse], error) {
	return p.client.ListMatchQueue(ctx, forward(ctx, req))
}

func (p *GameAdmin) RemoveFromMatchQueue(ctx context.Context, req *connect.Request[gamev1.RemoveFromMatchQueueRequest]) (*connect.Response[gamev1.RemoveFromMatchQueueResponse], error) {
	return p.client.RemoveFromMatchQueue(ctx, forward(ctx, req))
}

type ChatAdmin struct {
	client chatv1connect.ChatAdminServiceClient
}

var _ chatv1connect.ChatAdminServiceHandler = (*ChatAdmin)(nil)

func NewChatAdmin(client connect.HTTPClient, url string, opts ...connect.ClientOption) *ChatAdmin {
	return &ChatAdmin{client: chatv1connect.NewChatAdminServiceClient(client, url, opts...)}
}

func (p *ChatAdmin) ListChatRooms(ctx context.Context, req *connect.Request[chatv1.ListChatRoomsRequest]) (*connect.Response[chatv1.ListChatRoomsResponse], error) {
	return p.client.ListChatRooms(ctx, forward(ctx, req))
}

func (p *ChatAdmin) GetChatRoom(ctx context.Context, req *connect.Request[chatv1.GetChatRoomRequest]) (*connect.Response[chatv1.GetChatRoomResponse], error) {
	return p.client.GetChatRoom(ctx, forward(ctx, req))
}

func (p *ChatAdmin) ListChatRoomMessages(ctx context.Context, req *connect.Request[chatv1.ListChatRoomMessagesRequest]) (*connect.Response[chatv1.ListChatRoomMessagesResponse], error) {
	return p.client.ListChatRoomMessages(ctx, forward(ctx, req))
}

func (p *ChatAdmin) DeleteChatMessage(ctx context.Context, req *connect.Request[chatv1.DeleteChatMessageRequest]) (*connect.Response[chatv1.DeleteChatMessageResponse], error) {
	return p.client.DeleteChatMessage(ctx, forward(ctx, req))
}

func (p *ChatAdmin) RemoveChatMember(ctx context.Context, req *connect.Request[chatv1.RemoveChatMemberRequest]) (*connect.Response[chatv1.RemoveChatMemberResponse], error) {
	return p.client.RemoveChatMember(ctx, forward(ctx, req))
}

func (p *ChatAdmin) ListChatUploads(ctx context.Context, req *connect.Request[chatv1.ListChatUploadsRequest]) (*connect.Response[chatv1.ListChatUploadsResponse], error) {
	return p.client.ListChatUploads(ctx, forward(ctx, req))
}

type CallsAdmin struct {
	client callsv1connect.CallsAdminServiceClient
}

var _ callsv1connect.CallsAdminServiceHandler = (*CallsAdmin)(nil)

func NewCallsAdmin(client connect.HTTPClient, url string, opts ...connect.ClientOption) *CallsAdmin {
	return &CallsAdmin{client: callsv1connect.NewCallsAdminServiceClient(client, url, opts...)}
}

func (p *CallsAdmin) ListAllCalls(ctx context.Context, req *connect.Request[callv1.ListAllCallsRequest]) (*connect.Response[callv1.ListAllCallsResponse], error) {
	return p.client.ListAllCalls(ctx, forward(ctx, req))
}

func (p *CallsAdmin) TerminateCall(ctx context.Context, req *connect.Request[callv1.TerminateCallRequest]) (*connect.Response[callv1.TerminateCallResponse], error) {
	return p.client.TerminateCall(ctx, forward(ctx, req))
}

type RatingAdmin struct {
	client ratingv1connect.RatingAdminServiceClient
}

var _ ratingv1connect.RatingAdminServiceHandler = (*RatingAdmin)(nil)

func NewRatingAdmin(client connect.HTTPClient, url string, opts ...connect.ClientOption) *RatingAdmin {
	return &RatingAdmin{client: ratingv1connect.NewRatingAdminServiceClient(client, url, opts...)}
}

func (p *RatingAdmin) ListRatingChanges(ctx context.Context, req *connect.Request[ratingv1.ListRatingChangesRequest]) (*connect.Response[ratingv1.ListRatingChangesResponse], error) {
	return p.client.ListRatingChanges(ctx, forward(ctx, req))
}

func (p *RatingAdmin) ListRatings(ctx context.Context, req *connect.Request[ratingv1.ListRatingsRequest]) (*connect.Response[ratingv1.ListRatingsResponse], error) {
	return p.client.ListRatings(ctx, forward(ctx, req))
}

type TournamentAdmin struct {
	client tournamentv1connect.TournamentAdminServiceClient
}

var _ tournamentv1connect.TournamentAdminServiceHandler = (*TournamentAdmin)(nil)

func NewTournamentAdmin(client connect.HTTPClient, url string, opts ...connect.ClientOption) *TournamentAdmin {
	return &TournamentAdmin{client: tournamentv1connect.NewTournamentAdminServiceClient(client, url, opts...)}
}

func (p *TournamentAdmin) UpdateTournament(ctx context.Context, req *connect.Request[tournamentv1.UpdateTournamentRequest]) (*connect.Response[tournamentv1.UpdateTournamentResponse], error) {
	return p.client.UpdateTournament(ctx, forward(ctx, req))
}

func (p *TournamentAdmin) RemoveTournamentPlayer(ctx context.Context, req *connect.Request[tournamentv1.RemoveTournamentPlayerRequest]) (*connect.Response[tournamentv1.RemoveTournamentPlayerResponse], error) {
	return p.client.RemoveTournamentPlayer(ctx, forward(ctx, req))
}

type AnalysisAdmin struct {
	client analysisv1connect.AnalysisAdminServiceClient
}

var _ analysisv1connect.AnalysisAdminServiceHandler = (*AnalysisAdmin)(nil)

func NewAnalysisAdmin(client connect.HTTPClient, url string, opts ...connect.ClientOption) *AnalysisAdmin {
	return &AnalysisAdmin{client: analysisv1connect.NewAnalysisAdminServiceClient(client, url, opts...)}
}

func (p *AnalysisAdmin) ListAnalysisReports(ctx context.Context, req *connect.Request[analysisv1.ListAnalysisReportsRequest]) (*connect.Response[analysisv1.ListAnalysisReportsResponse], error) {
	return p.client.ListAnalysisReports(ctx, forward(ctx, req))
}
