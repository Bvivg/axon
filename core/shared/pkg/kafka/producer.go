package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

// ProducerConfig describes a producer. Brokers and Service are required.
type ProducerConfig struct {
	// Brokers is the bootstrap list, host:port each.
	Brokers []string
	// Service names the publishing service and is stamped on its log lines.
	Service string
	// WriteTimeout bounds a single publish on top of the caller's context, so a
	// stalled broker surfaces as an error instead of a stuck request.
	WriteTimeout time.Duration
	// BatchTimeout is how long the writer may hold a message waiting for
	// company. It is set explicitly because the library's default is a full
	// second: a lone chat message would sit in the buffer for that second
	// before anything downstream saw it.
	BatchTimeout time.Duration
}

// Producer defaults. They are named constants rather than literals buried in
// applyDefaults so a service that overrides one can see what it is overriding.
const (
	DefaultWriteTimeout = 10 * time.Second
	DefaultBatchTimeout = 10 * time.Millisecond
	// maxAttempts is the writer's internal retry budget for transient failures
	// such as a leader election. Past that the error belongs to the caller.
	maxAttempts = 3
)

func (c *ProducerConfig) applyDefaults() {
	if c.WriteTimeout == 0 {
		c.WriteTimeout = DefaultWriteTimeout
	}
	if c.BatchTimeout == 0 {
		c.BatchTimeout = DefaultBatchTimeout
	}
}

func (c ProducerConfig) validate() error {
	if len(c.Brokers) == 0 {
		return fmt.Errorf("kafka: producer: %w", ErrNoBrokers)
	}
	if c.Service == "" {
		return fmt.Errorf("kafka: producer: %w", ErrNoService)
	}
	return nil
}

// Producer publishes events onto the bus.
//
// It is safe for concurrent use and meant to be long-lived: one per service,
// built at startup and closed on shutdown.
type Producer struct {
	writer  *kafkago.Writer
	brokers []string
	logger  *slog.Logger
}

// NewProducer builds a producer. It does not talk to the brokers: refusing to
// start because a broker is briefly unreachable would turn a recoverable
// outage into a failed deploy. Readiness is Check's job.
func NewProducer(cfg ProducerConfig, log *slog.Logger) (*Producer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()

	writer := &kafkago.Writer{
		Addr: kafkago.TCP(cfg.Brokers...),
		// Hash on the key so one room's or one session's messages keep their
		// order by staying on a single partition.
		Balancer: &kafkago.Hash{},
		// Every in-sync replica must acknowledge. The cheaper acknowledgement
		// modes trade away exactly the durability that makes an event bus worth
		// having.
		RequiredAcks: kafkago.RequireAll,
		Async:        false,
		BatchTimeout: cfg.BatchTimeout,
		WriteTimeout: cfg.WriteTimeout,
		MaxAttempts:  maxAttempts,
		// The broker's own auto.create.topics.enable has the final say. Asking
		// for creation matters only in the local stack and in tests, where a
		// topic nobody has published to yet would fail the first publish; a real
		// cluster has auto-creation off and ignores this.
		AllowAutoTopicCreation: true,
		ErrorLogger:            errorPrinter(log),
		Logger:                 debugPrinter(log),
	}

	return &Producer{
		writer:  writer,
		brokers: cfg.Brokers,
		logger:  log.With("component", "kafka.producer", "service", cfg.Service),
	}, nil
}

// Publish sends one message and waits for the brokers to acknowledge it.
//
// The correlation ID travels in a header. When ctx carries none, one is
// generated: a message with no ID leaves everything the consumer does with it
// untraceable, whereas a generated ID at least ties this publish to that work.
func (p *Producer) Publish(ctx context.Context, msg Message) error {
	if err := msg.Topic.Validate(); err != nil {
		return err
	}

	ctx, id := correlation.Ensure(ctx)

	ctx, cancel := context.WithTimeout(ctx, p.writer.WriteTimeout)
	defer cancel()

	if err := p.writer.WriteMessages(ctx, msg.toKafka(id)); err != nil {
		return fmt.Errorf("kafka: publish to %s: %w", msg.Topic, err)
	}

	p.logger.DebugContext(ctx, "published", "topic", msg.Topic.String(), "bytes", len(msg.Value))
	return nil
}

// Close flushes what is still buffered and releases the connections. It is the
// graceful half of shutdown: without it a batch younger than BatchTimeout dies
// with the process.
func (p *Producer) Close() error {
	if err := p.writer.Close(); err != nil {
		return fmt.Errorf("kafka: close producer: %w", err)
	}
	return nil
}

// Name implements health.Checker.
func (p *Producer) Name() string { return "kafka" }

// Check implements health.Checker: readiness fails while the brokers are
// unreachable, which is what takes an unready replica out of rotation.
func (p *Producer) Check(ctx context.Context) error {
	return probeBrokers(ctx, p.brokers)
}

// probeBrokers asks for cluster metadata — the cheapest round trip that proves
// a broker is both reachable and answering.
func probeBrokers(ctx context.Context, brokers []string) error {
	client := &kafkago.Client{Addr: kafkago.TCP(brokers...)}
	if _, err := client.Metadata(ctx, &kafkago.MetadataRequest{}); err != nil {
		return fmt.Errorf("kafka: metadata: %w", err)
	}
	return nil
}
