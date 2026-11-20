package consumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/bvivg/axon/core/services/analysis/internal/review"
	"github.com/bvivg/axon/core/services/analysis/internal/store"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"
)

const Group = "analysis"

const (
	OutcomeReady     = "ready"
	OutcomeFailed    = "failed"
	OutcomeDuplicate = "duplicate"
	OutcomeSkipped   = "skipped"
	OutcomeInvalid   = "invalid"
)

var ErrNoMoves = errors.New("the game.finished event carries no moves")

type Store interface {
	Processed(ctx context.Context, event uuid.UUID) (bool, error)
	Start(ctx context.Context, job store.Job) (bool, error)
	Finish(ctx context.Context, job store.Job, outcome store.Outcome) (bool, error)
}

type Analyser interface {
	Analyse(ctx context.Context, moves []string) ([]review.Position, error)
}

type Metrics struct {
	Jobs     *prometheus.CounterVec
	Duration prometheus.Histogram
}

func NewMetrics(registry prometheus.Registerer) *Metrics {
	m := &Metrics{
		Jobs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "analysis_jobs_total",
			Help: "game.finished events seen by the analysis consumer, by outcome.",
		}, []string{"outcome"}),
		Duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "analysis_duration_seconds",
			Help:    "Engine time to analyse one finished chess game.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 4, 8, 15, 30, 60, 120},
		}),
	}
	registry.MustRegister(m.Jobs, m.Duration)
	return m
}

type Config struct {
	Brokers  []string
	Topic    string
	Group    string
	Store    Store
	Analyser Analyser
	Depth    int
	Engine   string
	Log      *slog.Logger
	Metrics  *Metrics
}

type Consumer struct {
	cfg Config
	log *slog.Logger
}

func New(cfg Config) *Consumer {
	if cfg.Group == "" {
		cfg.Group = Group
	}
	if cfg.Topic == "" {
		cfg.Topic = FinishedTopic
	}
	return &Consumer{cfg: cfg, log: cfg.Log.With("component", "consumer", "topic", cfg.Topic, "group", cfg.Group)}
}

func (c *Consumer) count(outcome string) {
	if c.cfg.Metrics != nil {
		c.cfg.Metrics.Jobs.WithLabelValues(outcome).Inc()
	}
}

var tracer = telemetry.Tracer("axon/analysis/consumer")

func header(record *kgo.Record, key string) string {
	for _, h := range record.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c *Consumer) Handle(ctx context.Context, record *kgo.Record) error {
	ctx, span := tracer.Start(ctx, "analysis.game", trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attribute.Int("messaging.destination.partition.id", int(record.Partition)), attribute.Int64("messaging.kafka.offset", record.Offset)))
	defer span.End()
	game, err := Decode(header(record, outbox.TypeHeader), record.Value)
	switch {
	case errors.Is(err, ErrNotChess), errors.Is(err, ErrForeign):
		c.count(OutcomeSkipped)
		return nil
	case err != nil:
		c.count(OutcomeInvalid)
		c.log.ErrorContext(ctx, "skipping invalid game.finished event", "partition", record.Partition, "offset", record.Offset,
			"event_id", header(record, outbox.IDHeader), "error", err)
		return nil
	}
	span.SetAttributes(attribute.String("axon.session_id", game.Session.String()), attribute.Int("axon.moves", len(game.Moves)))
	if err := c.Analyse(ctx, game); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("consumer: offset %d of partition %d: %w", record.Offset, record.Partition, err)
	}
	return nil
}

func (c *Consumer) Analyse(ctx context.Context, game Game) error {
	seen, err := c.cfg.Store.Processed(ctx, game.EventID)
	if err != nil {
		return err
	}
	if seen {
		c.count(OutcomeDuplicate)
		return nil
	}
	job := store.Job{Event: game.EventID, Session: game.Session, Moves: len(game.Moves), Depth: c.cfg.Depth, Engine: c.cfg.Engine, FinishedAt: game.FinishedAt}
	started, err := c.cfg.Store.Start(ctx, job)
	if err != nil {
		return err
	}
	if !started {
		c.count(OutcomeDuplicate)
		return nil
	}
	began := time.Now()
	var outcome store.Outcome
	if len(game.Moves) == 0 {
		outcome.Err = ErrNoMoves.Error()
	} else {
		positions, err := c.cfg.Analyser.Analyse(ctx, game.Moves)
		if ctx.Err() != nil {
			return fmt.Errorf("analysis of %s interrupted: %w", game.Session, ctx.Err())
		}
		if err != nil {
			outcome.Err = err.Error()
		}
		outcome.Positions = positions
	}
	outcome.Duration = time.Since(began)
	finished, err := c.cfg.Store.Finish(ctx, job, outcome)
	if err != nil {
		return err
	}
	if !finished {
		c.count(OutcomeDuplicate)
		return nil
	}
	if outcome.Err != "" {
		c.count(OutcomeFailed)
		trace.SpanFromContext(ctx).SetStatus(codes.Error, outcome.Err)
		c.log.WarnContext(ctx, "analysis failed", "session_id", game.Session, "event_id", game.EventID, "moves", len(game.Moves), "error", outcome.Err)
		return nil
	}
	c.count(OutcomeReady)
	if c.cfg.Metrics != nil {
		c.cfg.Metrics.Duration.Observe(outcome.Duration.Seconds())
	}
	c.log.InfoContext(ctx, "analysis ready", "session_id", game.Session, "event_id", game.EventID, "moves", len(game.Moves),
		"depth", c.cfg.Depth, "duration_ms", outcome.Duration.Milliseconds())
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
