package pubsub

import (
	"context"

	"github.com/google/uuid"
)

type CallsPublisher struct{ Bus UserSignalBus }

func (p CallsPublisher) PublishCall(ctx context.Context, user, event uuid.UUID) error {
	return p.Bus.PublishUserSignal(ctx, user, Signal{Kind: SignalCallState, UserID: user, EventID: event})
}
func (p CallsPublisher) PublishCallSignal(ctx context.Context, user, endpoint uuid.UUID, payload []byte) error {
	return p.Bus.PublishUserSignal(ctx, user, Signal{Kind: SignalCallPeer, UserID: user, EndpointID: endpoint, Payload: string(payload)})
}
