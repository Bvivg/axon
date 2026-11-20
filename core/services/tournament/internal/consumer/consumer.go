package consumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/bvivg/axon/core/services/tournament/internal/orchestrator"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"
)

type Recorder interface {
	Record(ctx context.Context, result orchestrator.Result) (string, error)
}

type RatingStore interface {
	SetRatings(ctx context.Context, game string, at time.Time, ratings map[uuid.UUID]int) error
}

type Config struct {
	Brokers       []string
	FinishedTopic string
	RatingTopic   string
	Group         string
	Recorder      Recorder
	Ratings       RatingStore
	Log           *slog.Logger
}

type Consumer struct {
	cfg Config
	log *slog.Logger
}

func New(cfg Config) *Consumer {
	if cfg.Group == "" {
		cfg.Group = Group
	}
	if cfg.FinishedTopic == "" {
		cfg.FinishedTopic = FinishedTopic
	}
	if cfg.RatingTopic == "" {
		cfg.RatingTopic = RatingTopic
	}
	return &Consumer{cfg: cfg, log: cfg.Log.With("component", "consumer", "group", cfg.Group)}
}

var tracer = telemetry.Tracer("axon/tournament/consumer")

func header(record *kgo.Record, key string) string {
	for _, h := range record.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c *Consumer) Handle(ctx context.Context, record *kgo.Record) error {
	kind := header(record, outbox.TypeHeader)
	switch {
	case kind == finishedType, kind == "" && record.Topic == c.cfg.FinishedTopic:
		return c.finished(ctx, record)
	case kind == ratingType, kind == "" && record.Topic == c.cfg.RatingTopic:
		return c.ratings(ctx, record)
	default:
		return nil
	}
}

func (c *Consumer) invalid(ctx context.Context, record *kgo.Record, err error) {
	c.log.ErrorContext(ctx, "skipping invalid event", "topic", record.Topic, "partition", record.Partition, "offset", record.Offset,
		"event_id", header(record, outbox.IDHeader), "error", err)
}

func (c *Consumer) finished(ctx context.Context, record *kgo.Record) error {
	result, err := DecodeFinished(record.Value)
	switch {
	case errors.Is(err, ErrForeign):
		return nil
	case err != nil:
		c.invalid(ctx, record, err)
		return nil
	}
	ctx, span := tracer.Start(ctx, "tournament.record", trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(
		attribute.String("tournament.id", result.Tournament.String()), attribute.String("game.session_id", result.Session.String()),
		attribute.Int("messaging.destination.partition.id", int(record.Partition)), attribute.Int64("messaging.kafka.offset", record.Offset)))
	defer span.End()
	outcome, err := c.cfg.Recorder.Record(ctx, result)
	if err != nil {
		return fmt.Errorf("consumer: offset %d of partition %d: %w", record.Offset, record.Partition, err)
	}
	span.SetAttributes(attribute.String("tournament.result", outcome))
	return nil
}

func (c *Consumer) ratings(ctx context.Context, record *kgo.Record) error {
	ratings, err := DecodeRatings(record.Value)
	if err != nil {
		c.invalid(ctx, record, err)
		return nil
	}
	if err := c.cfg.Ratings.SetRatings(ctx, ratings.Game, ratings.At, ratings.Ratings); err != nil {
		return fmt.Errorf("consumer: ratings at offset %d of partition %d: %w", record.Offset, record.Partition, err)
	}
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
		kgo.ConsumeTopics(c.cfg.FinishedTopic, c.cfg.RatingTopic),
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
			if ctx.Err() == nil && !errors.Is(err, kgo.ErrClientClosed) {
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
