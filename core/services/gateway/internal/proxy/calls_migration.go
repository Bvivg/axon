package proxy

import (
	"context"

	"connectrpc.com/connect"

	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
)

func (p *Calls) RequestCallMigration(ctx context.Context, req *connect.Request[callv1.RequestCallMigrationRequest]) (*connect.Response[callv1.RequestCallMigrationResponse], error) {
	return p.client.RequestCallMigration(ctx, forward(ctx, req))
}
func (p *Calls) ReportCallMigration(ctx context.Context, req *connect.Request[callv1.ReportCallMigrationRequest]) (*connect.Response[callv1.ReportCallMigrationResponse], error) {
	return p.client.ReportCallMigration(ctx, forward(ctx, req))
}
func (p *Calls) GetMigrationCredentials(ctx context.Context, req *connect.Request[callv1.GetMigrationCredentialsRequest]) (*connect.Response[callv1.GetMigrationCredentialsResponse], error) {
	return p.client.GetMigrationCredentials(ctx, forward(ctx, req))
}
