package rating

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/session"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"
)

const (
	Topic = "rating.changed.v1"
	Group = "game-ratings"
)

var eventType = string((&eventsv1.RatingChanged{}).ProtoReflect().Descriptor().FullName())

var ErrMalformed = errors.New("rating: malformed rating.changed event")

type Applier interface {
	ApplyRatings(ctx context.Context, update session.RatingUpdate) error
}

func Decode(value []byte, now time.Time) (session.RatingUpdate, error) {
	event := &eventsv1.RatingChanged{}
	if err := proto.Unmarshal(value, event); err != nil {
		return session.RatingUpdate{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if event.GetGame() == "" || len(event.GetChanges()) == 0 {
		return session.RatingUpdate{}, fmt.Errorf("%w: game and changes are required", ErrMalformed)
	}
	update := session.RatingUpdate{Game: engine.Key(event.GetGame()), At: now.UTC()}
	if event.GetChangedAt().IsValid() {
		update.At = event.GetChangedAt().AsTime().UTC()
	}
	if raw := event.GetSessionId(); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return session.RatingUpdate{}, fmt.Errorf("%w: session %q", ErrMalformed, raw)
		}
		update.Session = id
	}
	seen := map[uuid.UUID]bool{}
	for _, change := range event.GetChanges() {
		user, err := uuid.Parse(change.GetUserId())
		if err != nil || seen[user] {
			return session.RatingUpdate{}, fmt.Errorf("%w: user %q", ErrMalformed, change.GetUserId())
		}
		if change.GetGames() < 0 || change.GetWins() < 0 || change.GetLosses() < 0 || change.GetDraws() < 0 {
			return session.RatingUpdate{}, fmt.Errorf("%w: negative counters for %s", ErrMalformed, user)
		}
		seen[user] = true
		update.Players = append(update.Players, session.RatedPlayer{
			User: user, Before: int(change.GetBefore()), After: int(change.GetAfter()),
			Games: int(change.GetGames()), Wins: int(change.GetWins()), Losses: int(change.GetLosses()), Draws: int(change.GetDraws()),
		})
	}
	return update, nil
}

type Consumer struct {
	brokers []string
	topic   string
	store   Applier
	log     *slog.Logger
	now     func() time.Time
}

func NewConsumer(brokers []string, topic string, store Applier, log *slog.Logger) *Consumer {
	return &Consumer{brokers: brokers, topic: topic, store: store, log: log.With("component", "rating-consumer"), now: time.Now}
}

var tracer = telemetry.Tracer("axon/game/rating")

func header(record *kgo.Record, key string) string {
	for _, h := range record.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c *Consumer) Handle(ctx context.Context, record *kgo.Record) error {
	if kind := header(record, outbox.TypeHeader); kind != "" && kind != eventType {
		return nil
	}
	ctx, span := tracer.Start(ctx, "rating.changed apply")
	defer span.End()
	update, err := Decode(record.Value, c.now())
	if err != nil {
		c.log.ErrorContext(ctx, "skipping malformed rating event", "partition", record.Partition, "offset", record.Offset,
			"event_id", header(record, outbox.IDHeader), "error", err)
		return nil
	}
	if err := c.store.ApplyRatings(ctx, update); err != nil {
		return fmt.Errorf("rating: apply offset %d of partition %d: %w", record.Offset, record.Partition, err)
	}
	return nil
}

func (c *Consumer) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := c.session(ctx); err != nil && ctx.Err() == nil {
			c.log.WarnContext(ctx, "rating consumer restarting", "error", err)
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
		kgo.SeedBrokers(c.brokers...),
		kgo.ConsumerGroup(Group),
		kgo.ConsumeTopics(c.topic),
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
		if err := commit(ctx, client, done); err != nil && failure == nil {
			failure = err
		}
		client.AllowRebalance()
		if failure != nil {
			return failure
		}
	}
	return nil
}

func commit(ctx context.Context, client *kgo.Client, records []*kgo.Record) error {
	if len(records) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return client.CommitRecords(ctx, records...)
}
