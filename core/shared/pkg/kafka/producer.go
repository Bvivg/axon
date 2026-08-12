package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

type ProducerConfig struct {
	Brokers []string

	Service string

	WriteTimeout time.Duration

	BatchTimeout time.Duration
}

const (
	DefaultWriteTimeout = 10 * time.Second
	DefaultBatchTimeout = 10 * time.Millisecond

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

type Producer struct {
	writer  *kafkago.Writer
	brokers []string
	logger  *slog.Logger
}

func NewProducer(cfg ProducerConfig, log *slog.Logger) (*Producer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()

	writer := &kafkago.Writer{
		Addr: kafkago.TCP(cfg.Brokers...),

		Balancer: &kafkago.Hash{},

		RequiredAcks: kafkago.RequireAll,
		Async:        false,
		BatchTimeout: cfg.BatchTimeout,
		WriteTimeout: cfg.WriteTimeout,
		MaxAttempts:  maxAttempts,

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

func (p *Producer) Close() error {
	if err := p.writer.Close(); err != nil {
		return fmt.Errorf("kafka: close producer: %w", err)
	}
	return nil
}

func (p *Producer) Name() string { return "kafka" }

func (p *Producer) Check(ctx context.Context) error {
	return probeBrokers(ctx, p.brokers)
}

func probeBrokers(ctx context.Context, brokers []string) error {
	client := &kafkago.Client{Addr: kafkago.TCP(brokers...)}
	if _, err := client.Metadata(ctx, &kafkago.MetadataRequest{}); err != nil {
		return fmt.Errorf("kafka: metadata: %w", err)
	}
	return nil
}
