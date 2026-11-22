package auditlog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/authz"
	"github.com/bvivg/axon/core/shared/pkg/outbox"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
)

const Group = "auth-audit-log"

var (
	adminType    = string((&eventsv1.AdminAction{}).ProtoReflect().Descriptor().FullName())
	securityType = string((&eventsv1.SecurityEvent{}).ProtoReflect().Descriptor().FullName())

	ErrInvalid = errors.New("auditlog: invalid event")
	ErrSkipped = errors.New("auditlog: event is not part of the audit log")
)

type Store interface {
	InsertAuditEvent(ctx context.Context, e domain.AuditEvent) (bool, error)
}

type Config struct {
	Brokers []string
	Group   string
	Store   Store
	Log     *slog.Logger
	Events  *prometheus.CounterVec
}

type Consumer struct {
	cfg Config
	log *slog.Logger
}

func NewEventCounter(registry prometheus.Registerer) *prometheus.CounterVec {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "auth_audit_log_events_total",
		Help: "Audit events seen by the audit log consumer, by result.",
	}, []string{"result"})
	registry.MustRegister(counter)
	return counter
}

func New(cfg Config) *Consumer {
	if cfg.Group == "" {
		cfg.Group = Group
	}
	return &Consumer{cfg: cfg, log: cfg.Log.With("component", "auditlog", "group", cfg.Group)}
}

func (c *Consumer) count(result string) {
	if c.cfg.Events != nil {
		c.cfg.Events.WithLabelValues(result).Inc()
	}
}

func Decode(topic, kind string, value []byte) (domain.AuditEvent, error) {
	switch {
	case topic == authz.Topic && (kind == "" || kind == adminType):
		event := &eventsv1.AdminAction{}
		if err := proto.Unmarshal(value, event); err != nil {
			return domain.AuditEvent{}, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
		return fromAdmin(event)
	case topic == repository.SecurityAuditTopic && (kind == "" || kind == securityType):
		event := &eventsv1.SecurityEvent{}
		if err := proto.Unmarshal(value, event); err != nil {
			return domain.AuditEvent{}, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
		return fromSecurity(event)
	default:
		return domain.AuditEvent{}, fmt.Errorf("%w: %s %s", ErrSkipped, topic, kind)
	}
}

func fromAdmin(event *eventsv1.AdminAction) (domain.AuditEvent, error) {
	id, err := uuid.Parse(event.GetEventId())
	if err != nil || event.GetService() == "" || event.GetAction() == "" || event.GetOccurredAt() == nil {
		return domain.AuditEvent{}, fmt.Errorf("%w: admin action %q", ErrInvalid, event.GetEventId())
	}
	out := domain.AuditEvent{
		ID:            id,
		OccurredAt:    event.GetOccurredAt().AsTime(),
		Service:       event.GetService(),
		Action:        event.GetAction(),
		Outcome:       domain.AuditSucceeded,
		TargetType:    event.GetTargetType(),
		TargetID:      event.GetTargetId(),
		Reason:        event.GetReason(),
		CorrelationID: event.GetCorrelationId(),
		ClientIP:      event.GetClientIp(),
		UserAgent:     event.GetUserAgent(),
		Details:       event.GetDetails(),
		Changes:       fromChanges(event.GetChanges()),
	}
	switch event.GetOutcome() {
	case eventsv1.AdminOutcome_ADMIN_OUTCOME_DENIED:
		out.Outcome = domain.AuditDenied
	case eventsv1.AdminOutcome_ADMIN_OUTCOME_FAILED:
		out.Outcome = domain.AuditFailed
	}
	out.ActorID = optionalID(event.GetActorId())
	return out, nil
}

var securityActions = map[eventsv1.SecurityAction]string{
	eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_SUCCEEDED:       "login_succeeded",
	eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_FAILED:          "login_failed",
	eventsv1.SecurityAction_SECURITY_ACTION_LOGGED_OUT:            "logged_out",
	eventsv1.SecurityAction_SECURITY_ACTION_LOGGED_OUT_EVERYWHERE: "logged_out_everywhere",
	eventsv1.SecurityAction_SECURITY_ACTION_SESSION_REVOKED:       "session_revoked",
	eventsv1.SecurityAction_SECURITY_ACTION_REFRESH_TOKEN_REUSED:  "refresh_token_reused",
	eventsv1.SecurityAction_SECURITY_ACTION_EMAIL_CHANGED:         "email_changed",
}

func fromSecurity(event *eventsv1.SecurityEvent) (domain.AuditEvent, error) {
	action, ok := securityActions[event.GetAction()]
	if !ok {
		return domain.AuditEvent{}, fmt.Errorf("%w: security action %s", ErrSkipped, event.GetAction())
	}
	id, err := uuid.Parse(event.GetEventId())
	if err != nil || event.GetOccurredAt() == nil {
		return domain.AuditEvent{}, fmt.Errorf("%w: security event %q", ErrInvalid, event.GetEventId())
	}
	out := domain.AuditEvent{
		ID:         id,
		OccurredAt: event.GetOccurredAt().AsTime(),
		Service:    "auth",
		Action:     action,
		Outcome:    domain.AuditSucceeded,
		ClientIP:   event.GetClientIp(),
		UserAgent:  event.GetUserAgent(),
	}
	if action == "login_failed" {
		out.Outcome = domain.AuditFailed
	}
	if event.GetUserId() != "" {
		out.TargetType, out.TargetID = "user", event.GetUserId()
	}
	out.ActorID = optionalID(event.GetActorId())
	if out.ActorID == nil && action != "login_failed" {
		out.ActorID = optionalID(event.GetUserId())
	}
	return out, nil
}

func optionalID(raw string) *uuid.UUID {
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return &id
}

func header(record *kgo.Record, key string) string {
	for _, h := range record.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c *Consumer) Handle(ctx context.Context, record *kgo.Record) error {
	event, err := Decode(record.Topic, header(record, outbox.TypeHeader), record.Value)
	switch {
	case errors.Is(err, ErrSkipped):
		c.count("skipped")
		return nil
	case err != nil:
		c.count("invalid")
		c.log.ErrorContext(ctx, "skipping an invalid audit event", "topic", record.Topic, "partition", record.Partition,
			"offset", record.Offset, "event_id", header(record, outbox.IDHeader), "error", err)
		return nil
	}
	inserted, err := c.cfg.Store.InsertAuditEvent(ctx, event)
	if err != nil {
		return fmt.Errorf("auditlog: offset %d of %s/%d: %w", record.Offset, record.Topic, record.Partition, err)
	}
	if inserted {
		c.count("recorded")
	} else {
		c.count("duplicate")
	}
	return nil
}

func (c *Consumer) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := c.session(ctx); err != nil && ctx.Err() == nil {
			c.log.WarnContext(ctx, "audit log consumer restarting", "error", err)
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}
	return nil
}

func (c *Consumer) session(ctx context.Context) error {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(c.cfg.Brokers...),
		kgo.ConsumerGroup(c.cfg.Group),
		kgo.ConsumeTopics(authz.Topic, repository.SecurityAuditTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.FetchMaxWait(500*time.Millisecond),
	)
	if err != nil {
		return err
	}
	defer client.Close()
	for ctx.Err() == nil {
		fetches := client.PollFetches(ctx)
		var failure error
		fetches.EachError(func(_ string, _ int32, err error) {
			if ctx.Err() == nil {
				failure = err
			}
		})
		var done []*kgo.Record
		if failure == nil {
			fetches.EachRecord(func(record *kgo.Record) {
				if failure != nil {
					return
				}
				if failure = c.Handle(ctx, record); failure == nil {
					done = append(done, record)
				}
			})
		}
		if len(done) > 0 {
			commit, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if err := client.CommitRecords(commit, done...); err != nil && failure == nil {
				failure = err
			}
			cancel()
		}
		client.AllowRebalance()
		if failure != nil {
			return failure
		}
	}
	return nil
}

func fromChanges(changes []*eventsv1.AdminFieldChange) []domain.AuditChange {
	if len(changes) == 0 {
		return nil
	}
	out := make([]domain.AuditChange, len(changes))
	for i, c := range changes {
		out[i] = domain.AuditChange{Field: c.GetField(), Before: c.GetBefore(), After: c.GetAfter()}
	}
	return out
}
