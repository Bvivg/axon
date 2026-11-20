package proxy

import (
	"context"

	"connectrpc.com/connect"

	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
)

func (p *Calls) GetCallCapabilities(ctx context.Context, req *connect.Request[callv1.GetCallCapabilitiesRequest]) (*connect.Response[callv1.GetCallCapabilitiesResponse], error) {
	return p.client.GetCallCapabilities(ctx, forward(ctx, req))
}
func (p *Calls) StartGroupCall(ctx context.Context, req *connect.Request[callv1.StartGroupCallRequest]) (*connect.Response[callv1.StartGroupCallResponse], error) {
	return p.client.StartGroupCall(ctx, forward(ctx, req))
}
func (p *Calls) StartGroupCallFromCall(ctx context.Context, req *connect.Request[callv1.StartGroupCallFromCallRequest]) (*connect.Response[callv1.StartGroupCallFromCallResponse], error) {
	return p.client.StartGroupCallFromCall(ctx, forward(ctx, req))
}
func (p *Calls) InviteCallParticipant(ctx context.Context, req *connect.Request[callv1.InviteCallParticipantRequest]) (*connect.Response[callv1.InviteCallParticipantResponse], error) {
	return p.client.InviteCallParticipant(ctx, forward(ctx, req))
}
func (p *Calls) LeaveCall(ctx context.Context, req *connect.Request[callv1.LeaveCallRequest]) (*connect.Response[callv1.LeaveCallResponse], error) {
	return p.client.LeaveCall(ctx, forward(ctx, req))
}
func (p *Calls) RemoveCallParticipant(ctx context.Context, req *connect.Request[callv1.RemoveCallParticipantRequest]) (*connect.Response[callv1.RemoveCallParticipantResponse], error) {
	return p.client.RemoveCallParticipant(ctx, forward(ctx, req))
}
func (p *Calls) GetSfuCredentials(ctx context.Context, req *connect.Request[callv1.GetSfuCredentialsRequest]) (*connect.Response[callv1.GetSfuCredentialsResponse], error) {
	return p.client.GetSfuCredentials(ctx, forward(ctx, req))
}
