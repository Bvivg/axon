package calls

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Store interface {
	BindEndpoint(context.Context, Actor, EndpointRef, uuid.UUID, time.Time) error
	RegisterEndpoint(context.Context, Actor, *EndpointRef, string, time.Time) (Endpoint, error)
	Endpoint(context.Context, Actor, EndpointRef, time.Time, bool) (Endpoint, error)
	Mutate(context.Context, Actor, Command, time.Time, func(*Call) (bool, error)) (Call, error)
	GetCall(context.Context, uuid.UUID, uuid.UUID) (Call, error)
	ActiveCall(context.Context, uuid.UUID) (*Call, error)
	ListCalls(context.Context, uuid.UUID, uuid.UUID, int) ([]Call, error)
	Pending(context.Context, Actor, EndpointRef, time.Time) ([]Event, error)
	Acknowledge(context.Context, Actor, EndpointRef, uuid.UUID, time.Time) error
	Event(context.Context, Actor, EndpointRef, uuid.UUID, time.Time) (Event, error)
	DueEvents(context.Context, time.Time) ([]Event, error)
	Expiring(context.Context, time.Time) ([]Command, error)
}
