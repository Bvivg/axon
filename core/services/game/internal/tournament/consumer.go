package tournament

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/attribute"

	"github.com/bvivg/axon/core/shared/pkg/outbox"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"
)

const (
	ResultApplied   = "applied"
	ResultSkipped   = "skipped"
	ResultInvalid   = "invalid"
	CommandRound    = "round_started"
	CommandForfeit  = "forfeit"
	CommandCancel   = "cancelled"
	CommandUnknown  = "unknown"
	restartInterval = time.Second
)

func NewCounter(registry prometheus.Registerer) *prometheus.CounterVec {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "game_tournament_commands_total",
		Help: "Tournament commands handled by the game service, by command and result.",
	}, []string{"command", "result"})
	registry.MustRegister(counter)
	return counter
}

type Config struct {
	Brokers       []string
	Topics        []string
	Group         string
	Games         Games
	Cancellations Cancellations
	Log           *slog.Logger
	Commands      *prometheus.CounterVec
}

type Consumer struct {
	cfg Config
	log *slog.Logger
}

func NewConsumer(cfg Config) *Consumer {
	if cfg.Group == "" {
		cfg.Group = Group
	}
	if len(cfg.Topics) == 0 {
		cfg.Topics = []string{RoundStartedTopic, ForfeitTopic, CancelledTopic}
	}
	return &Consumer{cfg: cfg, log: cfg.Log.With("component", "tournament-consumer", "group", cfg.Group)}
}

var tracer = telemetry.Tracer("axon/game/tournament")

func header(record *kgo.Record, key string) string {
	for _, h := range record.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c *Consumer) count(command, result string) {
	if c.cfg.Commands != nil {
		c.cfg.Commands.WithLabelValues(command, result).Inc()
	}
}

func name(command Command) string {
	switch {
	case command.Cancelled:
		return CommandCancel
	case command.Forfeit:
		return CommandForfeit
	default:
		return CommandRound
	}
}

func (c *Consumer) Handle(ctx context.Context, record *kgo.Record) error {
	ctx, span := tracer.Start(ctx, "tournament.command "+record.Topic)
	defer span.End()
	span.SetAttributes(attribute.Int("messaging.destination.partition.id", int(record.Partition)), attribute.Int64("messaging.kafka.offset", record.Offset))
	command, err := Decode(record.Topic, header(record, outbox.TypeHeader), record.Value)
	if err != nil {
		c.count(CommandUnknown, ResultInvalid)
		c.log.ErrorContext(ctx, "skipping malformed tournament command", "topic", record.Topic, "partition", record.Partition,
			"offset", record.Offset, "event_id", header(record, outbox.IDHeader), "error", err)
		return nil
	}
	kind := name(command)
	if err := c.apply(ctx, command); err != nil {
		if errors.Is(err, errCancelled) {
			c.count(kind, ResultSkipped)
			c.log.InfoContext(ctx, "tournament command for a cancelled tournament skipped", "tournament_id", command.Tournament, "command", kind)
			return nil
		}
		return fmt.Errorf("tournament: %s offset %d of partition %d: %w", record.Topic, record.Offset, record.Partition, err)
	}
	c.count(kind, ResultApplied)
	c.log.InfoContext(ctx, "tournament command applied", "tournament_id", command.Tournament, "command", kind, "games", len(command.Games), "aborted", len(command.Aborted))
	return nil
}

var errCancelled = errors.New("tournament: the tournament is cancelled")

func (c *Consumer) apply(ctx context.Context, command Command) error {
	if command.Cancelled {
		if err := c.cfg.Cancellations.Cancel(ctx, command.Tournament); err != nil {
			return err
		}
		for _, id := range command.Aborted {
			if err := c.cfg.Games.AbortTournamentGame(ctx, id); err != nil {
				return fmt.Errorf("abort %s: %w", id, err)
			}
		}
		return nil
	}
	cancelled, err := c.cfg.Cancellations.Cancelled(ctx, command.Tournament)
	if err != nil {
		return err
	}
	if cancelled {
		return errCancelled
	}
	for _, game := range command.Games {
		if command.Forfeit {
			err = c.cfg.Games.ForfeitTournamentGame(ctx, game)
		} else {
			err = c.cfg.Games.StartTournamentGame(ctx, game)
		}
		if err != nil {
			return fmt.Errorf("board %d of round %d: %w", game.Board, game.Round, err)
		}
	}
	return nil
}

func (c *Consumer) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := c.session(ctx); err != nil && ctx.Err() == nil {
			c.log.WarnContext(ctx, "tournament consumer restarting", "error", err)
			select {
			case <-ctx.Done():
			case <-time.After(restartInterval):
			}
		}
	}
	return nil
}

func (c *Consumer) session(ctx context.Context) error {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(c.cfg.Brokers...),
		kgo.ConsumerGroup(c.cfg.Group),
		kgo.ConsumeTopics(c.cfg.Topics...),
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
