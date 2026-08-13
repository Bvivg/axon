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

type ConsumerConfig struct {
	Brokers []string

	Service string

	Topics []Topic

	MaxWait time.Duration

	CommitTimeout time.Duration
}

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

func (c ConsumerConfig) GroupID() string { return c.Service }

type Handler func(ctx context.Context, msg Message) error

type Consumer struct {
	reader        *kafkago.Reader
	group         string
	commitTimeout time.Duration
	logger        *slog.Logger
}

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

		StartOffset: kafkago.FirstOffset,

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

func (c *Consumer) commit(ctx context.Context, raw kafkago.Message) error {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.commitTimeout)
	defer cancel()

	if err := c.reader.CommitMessages(commitCtx, raw); err != nil {
		return fmt.Errorf("kafka: commit %s offset %d: %w", raw.Topic, raw.Offset, err)
	}
	return nil
}

func (c *Consumer) stopping(ctx context.Context, err error) bool {
	return ctx.Err() != nil ||
		errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, io.EOF)
}

func (c *Consumer) Close() error {
	if err := c.reader.Close(); err != nil {
		return fmt.Errorf("kafka: close consumer: %w", err)
	}
	return nil
}

func (c *Consumer) Name() string { return "kafka" }

func (c *Consumer) Check(ctx context.Context) error {
	return probeBrokers(ctx, c.reader.Config().Brokers)
}
