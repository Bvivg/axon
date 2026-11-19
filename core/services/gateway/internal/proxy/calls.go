package proxy

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1/callsv1connect"
)

type Calls struct {
	client callsv1connect.CallsServiceClient
}

var _ callsv1connect.CallsServiceHandler = (*Calls)(nil)

func NewCalls(client connect.HTTPClient, url string, opts ...connect.ClientOption) (*Calls, error) {
	if client == nil || url == "" {
		return nil, errors.New("proxy: calls upstream required")
	}
	return &Calls{client: callsv1connect.NewCallsServiceClient(client, url, opts...)}, nil
}

var _ connect.HTTPClient = (*http.Client)(nil)

func (p *Calls) RegisterEndpoint(ctx context.Context, req *connect.Request[callv1.RegisterEndpointRequest]) (*connect.Response[callv1.RegisterEndpointResponse], error) {
	return p.client.RegisterEndpoint(ctx, forward(ctx, req))
}
func (p *Calls) StartCall(ctx context.Context, req *connect.Request[callv1.StartCallRequest]) (*connect.Response[callv1.StartCallResponse], error) {
	return p.client.StartCall(ctx, forward(ctx, req))
}
func (p *Calls) StartDirectCall(ctx context.Context, req *connect.Request[callv1.StartDirectCallRequest]) (*connect.Response[callv1.StartDirectCallResponse], error) {
	return p.client.StartDirectCall(ctx, forward(ctx, req))
}
func (p *Calls) AcceptCall(ctx context.Context, req *connect.Request[callv1.AcceptCallRequest]) (*connect.Response[callv1.AcceptCallResponse], error) {
	return p.client.AcceptCall(ctx, forward(ctx, req))
}
func (p *Calls) DeclineCall(ctx context.Context, req *connect.Request[callv1.DeclineCallRequest]) (*connect.Response[callv1.DeclineCallResponse], error) {
	return p.client.DeclineCall(ctx, forward(ctx, req))
}
func (p *Calls) CancelCall(ctx context.Context, req *connect.Request[callv1.CancelCallRequest]) (*connect.Response[callv1.CancelCallResponse], error) {
	return p.client.CancelCall(ctx, forward(ctx, req))
}
func (p *Calls) EndCall(ctx context.Context, req *connect.Request[callv1.EndCallRequest]) (*connect.Response[callv1.EndCallResponse], error) {
	return p.client.EndCall(ctx, forward(ctx, req))
}
func (p *Calls) ReportConnection(ctx context.Context, req *connect.Request[callv1.ReportConnectionRequest]) (*connect.Response[callv1.ReportConnectionResponse], error) {
	return p.client.ReportConnection(ctx, forward(ctx, req))
}
func (p *Calls) AcknowledgeCleanup(ctx context.Context, req *connect.Request[callv1.AcknowledgeCleanupRequest]) (*connect.Response[callv1.AcknowledgeCleanupResponse], error) {
	return p.client.AcknowledgeCleanup(ctx, forward(ctx, req))
}
func (p *Calls) GetCall(ctx context.Context, req *connect.Request[callv1.GetCallRequest]) (*connect.Response[callv1.GetCallResponse], error) {
	return p.client.GetCall(ctx, forward(ctx, req))
}
func (p *Calls) GetActiveCall(ctx context.Context, req *connect.Request[callv1.GetActiveCallRequest]) (*connect.Response[callv1.GetActiveCallResponse], error) {
	return p.client.GetActiveCall(ctx, forward(ctx, req))
}
func (p *Calls) ListCalls(ctx context.Context, req *connect.Request[callv1.ListCallsRequest]) (*connect.Response[callv1.ListCallsResponse], error) {
	return p.client.ListCalls(ctx, forward(ctx, req))
}
func (p *Calls) GetMediaCredentials(ctx context.Context, req *connect.Request[callv1.GetMediaCredentialsRequest]) (*connect.Response[callv1.GetMediaCredentialsResponse], error) {
	return p.client.GetMediaCredentials(ctx, forward(ctx, req))
}
