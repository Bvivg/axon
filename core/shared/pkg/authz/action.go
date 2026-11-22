package authz

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/correlation"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

const Topic = "admin.audit.v1"

type Outcome int

const (
	Succeeded Outcome = iota + 1
	Denied
	Failed
)

type Change struct {
	Field  string
	Before string
	After  string
}

type Action struct {
	Service    string
	Name       string
	Outcome    Outcome
	ActorID    uuid.UUID
	TargetType string
	TargetID   string
	Reason     string
	Details    map[string]string
	Changes    []Change

	CorrelationID string
	ClientIP      string
	UserAgent     string

	At time.Time
}

func NewAction(ctx context.Context, service, name, targetType, targetID, reason string) Action {
	action := Action{
		Service:       service,
		Name:          name,
		Outcome:       Succeeded,
		TargetType:    targetType,
		TargetID:      targetID,
		Reason:        reason,
		CorrelationID: correlation.FromContext(ctx),
		At:            time.Now().UTC(),
	}
	if actor, ok := ActorFrom(ctx); ok {
		action.ActorID = actor.UserID
		action.ClientIP = actor.ClientIP
		action.UserAgent = actor.UserAgent
	}
	return action
}

func (a Action) Event(id uuid.UUID) outbox.Event {
	aggregate := a.TargetID
	if aggregate == "" {
		aggregate = a.Service
	}
	actor := ""
	if a.ActorID != uuid.Nil {
		actor = a.ActorID.String()
	}
	return outbox.Event{
		ID:          id,
		Topic:       Topic,
		AggregateID: aggregate,
		Payload: &eventsv1.AdminAction{
			EventId:       id.String(),
			Service:       a.Service,
			Action:        a.Name,
			Outcome:       outcome(a.Outcome),
			ActorId:       actor,
			TargetType:    a.TargetType,
			TargetId:      a.TargetID,
			Reason:        a.Reason,
			CorrelationId: a.CorrelationID,
			ClientIp:      a.ClientIP,
			UserAgent:     a.UserAgent,
			Details:       a.Details,
			OccurredAt:    timestamppb.New(a.At),
			Changes:       changes(a.Changes),
		},
	}
}

func changes(list []Change) []*eventsv1.AdminFieldChange {
	if len(list) == 0 {
		return nil
	}
	out := make([]*eventsv1.AdminFieldChange, len(list))
	for i, c := range list {
		out[i] = &eventsv1.AdminFieldChange{Field: c.Field, Before: c.Before, After: c.After}
	}
	return out
}

func outcome(o Outcome) eventsv1.AdminOutcome {
	switch o {
	case Succeeded:
		return eventsv1.AdminOutcome_ADMIN_OUTCOME_SUCCEEDED
	case Denied:
		return eventsv1.AdminOutcome_ADMIN_OUTCOME_DENIED
	case Failed:
		return eventsv1.AdminOutcome_ADMIN_OUTCOME_FAILED
	default:
		return eventsv1.AdminOutcome_ADMIN_OUTCOME_UNSPECIFIED
	}
}

const (
	MinReason = 3
	MaxReason = 500
)

var ErrReason = errors.New("reason must be between 3 and 500 characters")

func Reason(raw string) (string, error) {
	reason := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(reason); n < MinReason || n > MaxReason {
		return "", connect.NewError(connect.CodeInvalidArgument, ErrReason)
	}
	return reason, nil
}
