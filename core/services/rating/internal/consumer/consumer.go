package consumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	"github.com/bvivg/axon/core/services/rating/internal/store"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"
)

const Group = "rating"

type Store interface {
	Apply(ctx context.Context, game ledger.Game) (store.Applied, bool, error)
}

type Board interface {
	Update(ctx context.Context, game string, players ...ledger.Player) error
}

type Config struct {
	Brokers []string
	Topic   string
	Group   string
	Store   Store
	Board   Board
	Log     *slog.Logger
	Events  *prometheus.CounterVec
}

type Consumer struct {
	cfg   Config
	log   *slog.Logger
	stale atomic.Bool
}

func NewEventCounter(registry prometheus.Registerer) *prometheus.CounterVec {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rating_events_total",
		Help: "game.finished events seen by the rating consumer, by result.",
	}, []string{"result"})
	registry.MustRegister(counter)
	return counter
}

func New(cfg Config) *Consumer {
	if cfg.Group == "" {
		cfg.Group = Group
	}
	if cfg.Topic == "" {
		cfg.Topic = ledger.FinishedTopic
	}
	return &Consumer{cfg: cfg, log: cfg.Log.With("component", "consumer", "topic", cfg.Topic, "group", cfg.Group)}
}

func (c *Consumer) count(result string) {
	if c.cfg.Events != nil {
		c.cfg.Events.WithLabelValues(result).Inc()
	}
}

func (c *Consumer) Stale() bool { return c.stale.Swap(false) }

var tracer = telemetry.Tracer("axon/rating/consumer")

func header(record *kgo.Record, key string) string {
	for _, h := range record.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c *Consumer) Handle(ctx context.Context, record *kgo.Record) error {
	ctx, span := tracer.Start(ctx, "rating.apply", trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attribute.Int("messaging.destination.partition.id", int(record.Partition)), attribute.Int64("messaging.kafka.offset", record.Offset)))
	defer span.End()
	game, err := ledger.Decode(header(record, outbox.TypeHeader), record.Value)
	switch {
	case errors.Is(err, ledger.ErrUnrated), errors.Is(err, ledger.ErrForeign):
		c.count("skipped")
		return nil
	case err != nil:
		c.count("invalid")
		c.log.ErrorContext(ctx, "skipping invalid game.finished event", "partition", record.Partition, "offset", record.Offset,
			"event_id", header(record, outbox.IDHeader), "error", err)
		return nil
	}
	applied, ok, err := c.cfg.Store.Apply(ctx, game)
	if err != nil {
		return fmt.Errorf("consumer: offset %d of partition %d: %w", record.Offset, record.Partition, err)
	}
	if !ok {
		c.count("duplicate")
		return nil
	}
	c.count("applied")
	if err := c.cfg.Board.Update(ctx, game.Game, applied.After[0], applied.After[1]); err != nil {
		c.stale.Store(true)
		c.log.WarnContext(ctx, "leaderboard update failed, a resync is scheduled", "game", game.Game, "error", err)
	}
	c.log.InfoContext(ctx, "rating settled", "session_id", game.Session, "event_id", game.EventID, "game", game.Game)
	return nil
}

func (c *Consumer) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := c.session(ctx); err != nil && ctx.Err() == nil {
			c.log.WarnContext(ctx, "consumer restarting", "error", err)
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
		kgo.ConsumeTopics(c.cfg.Topic),
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
