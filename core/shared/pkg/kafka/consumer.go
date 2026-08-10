package kafka

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

// ConsumerConfig describes a consumer. Brokers, Service and Topics are
// required.
type ConsumerConfig struct {
	// Brokers is the bootstrap list, host:port each.
	Brokers []string
	// Service names the CONSUMING service, and the consumer group is derived
	// from it. rules/infra.md forbids sharing a group between services; making
	// the group a function of the service name means a copy-pasted config
	// cannot break that rule quietly.
	Service string
	// Topics is the subscription. One consumer per service covering every topic
	// it reads keeps all of the service's members on one subscription, which is
	// what the group protocol expects.
	Topics []Topic
	// MaxWait bounds how long a fetch waits for new data before coming back
	// empty. It is set explicitly because the library waits 10s by default,
	// which delays a clean shutdown by up to that long.
	MaxWait time.Duration
	// CommitTimeout bounds committing an offset once the handler has succeeded.
	CommitTimeout time.Duration
}

// Consumer defaults.
const (
	DefaultMaxWait       = 500 * time.Millisecond
	DefaultCommitTimeout = 5 * time.Second
)

func (c *ConsumerConfig) applyDefaults() {
	if c.MaxWait == 0 {
		c.MaxWait = DefaultMaxWait
	}
	if c.CommitTimeout == 0 {
		c.CommitTimeout = DefaultCommitTimeout
	}
}

func (c ConsumerConfig) validate() error {
	if len(c.Brokers) == 0 {
		return fmt.Errorf("kafka: consumer: %w", ErrNoBrokers)
	}
	if c.Service == "" {
		return fmt.Errorf("kafka: consumer: %w", ErrNoService)
	}
	return validateTopics(c.Topics)
}

// GroupID is the consumer group this configuration joins: the consuming
// service's name, verbatim.
func (c ConsumerConfig) GroupID() string { return c.Service }

// Handler processes one message. The context it receives carries the message's
// correlation ID, so anything the handler logs or calls downstream stays tied
// to whatever produced the event.
//
// Returning an error stops the consumer and leaves the offset uncommitted — see
// Run.
type Handler func(ctx context.Context, msg Message) error

// Consumer reads events off the bus for one service.
//
// Delivery is at-least-once: an offset is committed only after the handler has
// returned successfully, so a crash between handling and committing redelivers
// the message. Handlers must therefore be idempotent.
type Consumer struct {
	reader        *kafkago.Reader
	group         string
	commitTimeout time.Duration
	logger        *slog.Logger
}

// NewConsumer builds a consumer. Like NewProducer it does not contact the
// brokers; joining the group happens on the first fetch inside Run.
func NewConsumer(cfg ConsumerConfig, log *slog.Logger) (*Consumer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     cfg.Brokers,
		GroupID:     cfg.GroupID(),
		GroupTopics: topicNames(cfg.Topics),
		MaxWait:     cfg.MaxWait,
		// A new group starts at the beginning of the topic. An event bus exists
		// so that a consumer added today can still see what happened before it
		// was deployed.
		StartOffset: kafkago.FirstOffset,
		// Zero disables the library's periodic auto-commit, which is the whole
		// point: offsets move only through CommitMessages, after the handler
		// has succeeded.
		CommitInterval: 0,
		ErrorLogger:    errorPrinter(log),
		Logger:         debugPrinter(log),
	})

	return &Consumer{
		reader:        reader,
		group:         cfg.GroupID(),
		commitTimeout: cfg.CommitTimeout,
		logger: log.With(
			"component", "kafka.consumer",
			"service", cfg.Service,
			"group", cfg.GroupID(),
		),
	}, nil
}

// Run fetches messages and hands them to h until ctx is cancelled or something
// fails. Cancellation is a clean stop and returns nil — a service shutting down
// should not look like an incident in the logs.
//
// A handler error stops the loop and is returned wrapped, with the offset left
// uncommitted so the message comes back. The alternative — log it and move on —
// loses the message silently, because the next successful commit jumps over the
// offset that failed. What to do about a failure (retry, drop, dead-letter) is
// the consuming service's decision, not the transport's.
func (c *Consumer) Run(ctx context.Context, h Handler) error {
	if h == nil {
		return fmt.Errorf("kafka: consumer: %w", ErrNoHandler)
	}

	c.logger.InfoContext(ctx, "consumer started")
	defer c.logger.InfoContext(ctx, "consumer stopped")

	for {
		raw, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if c.stopping(ctx, err) {
				return nil
			}
			return fmt.Errorf("kafka: fetch for group %s: %w", c.group, err)
		}

		msg := fromKafka(raw)
		msgCtx, _ := msg.Context(ctx)

		if err := h(msgCtx, msg); err != nil {
			c.logger.ErrorContext(msgCtx, "handler failed, offset not committed",
				"topic", msg.Topic.String(), "partition", msg.Partition, "offset", msg.Offset,
				"error", err)
			return fmt.Errorf("kafka: handle %s offset %d: %w", msg.Topic, msg.Offset, err)
		}

		if err := c.commit(msgCtx, raw); err != nil {
			return err
		}
	}
}

// commit acknowledges a handled message.
//
// The commit deliberately outlives a cancelled ctx: the work is already done,
// and dropping the commit because shutdown started would replay the message on
// the next boot for no reason.
func (c *Consumer) commit(ctx context.Context, raw kafkago.Message) error {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.commitTimeout)
	defer cancel()

	if err := c.reader.CommitMessages(commitCtx, raw); err != nil {
		return fmt.Errorf("kafka: commit %s offset %d: %w", raw.Topic, raw.Offset, err)
	}
	return nil
}

// stopping reports whether err is the ordinary end of the loop rather than a
// failure: the caller cancelled ctx, or Close shut the reader down underneath
// us (which the library reports as a closed pipe).
func (c *Consumer) stopping(ctx context.Context, err error) bool {
	return ctx.Err() != nil ||
		errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, io.EOF)
}

// Close leaves the consumer group and releases the connections. Leaving
// explicitly is what lets the group rebalance immediately instead of waiting
// for the session timeout to expire.
func (c *Consumer) Close() error {
	if err := c.reader.Close(); err != nil {
		return fmt.Errorf("kafka: close consumer: %w", err)
	}
	return nil
}

// Name implements health.Checker.
func (c *Consumer) Name() string { return "kafka" }

// Check implements health.Checker.
func (c *Consumer) Check(ctx context.Context) error {
	return probeBrokers(ctx, c.reader.Config().Brokers)
}
