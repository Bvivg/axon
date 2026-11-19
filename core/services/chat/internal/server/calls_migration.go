package server

import (
	"context"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
)

func (h *CallsHandler) RequestCallMigration(ctx context.Context, req *connect.Request[callv1.RequestCallMigrationRequest]) (*connect.Response[callv1.RequestCallMigrationResponse], error) {
	call, err := h.control(ctx, req.Header(), calls.Command{Action: "migrate", ID: callID(req.Msg.CallId), Endpoint: calls.RefFromProto(req.Msg.Endpoint), RequestID: callID(req.Msg.RequestId), Generation: req.Msg.Generation})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.RequestCallMigrationResponse{Call: call}), nil
}
func (h *CallsHandler) ReportCallMigration(ctx context.Context, req *connect.Request[callv1.ReportCallMigrationRequest]) (*connect.Response[callv1.ReportCallMigrationResponse], error) {
	action := map[callv1.MigrationReport]string{callv1.MigrationReport_MIGRATION_REPORT_READY: "migration_ready", callv1.MigrationReport_MIGRATION_REPORT_SWITCHED: "migration_switched", callv1.MigrationReport_MIGRATION_REPORT_CLEANUP: "migration_clean", callv1.MigrationReport_MIGRATION_REPORT_FAILED: "migration_failed", callv1.MigrationReport_MIGRATION_REPORT_ABORT_CLEANUP: "migration_abort_clean"}[req.Msg.Report]
	if action == "" {
		return nil, h.failure(ctx, calls.ErrInvalid)
	}
	call, err := h.control(ctx, req.Header(), calls.Command{Action: action, ID: callID(req.Msg.CallId), Endpoint: calls.RefFromProto(req.Msg.Endpoint), RequestID: callID(req.Msg.RequestId), MigrationID: callID(req.Msg.MigrationId), Generation: req.Msg.Generation, MediaVersion: req.Msg.MediaVersion, Ready: req.Msg.SourceHealthy})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.ReportCallMigrationResponse{Call: call}), nil
}
func (h *CallsHandler) GetMigrationCredentials(ctx context.Context, req *connect.Request[callv1.GetMigrationCredentialsRequest]) (*connect.Response[callv1.GetMigrationCredentialsResponse], error) {
	actor, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	url, token, servers, expires, err := h.service.MigrationCredentials(ctx, actor, calls.RefFromProto(req.Msg.Endpoint), callID(req.Msg.CallId), callID(req.Msg.MigrationId), req.Msg.Generation)
	if err != nil {
		return nil, h.failure(ctx, err)
	}
	return connect.NewResponse(&callv1.GetMigrationCredentialsResponse{Url: url, Token: token, IceServers: servers, ExpiresAt: calls.LeaseProto(expires)}), nil
}
