package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/journal"
)

type config struct {
	results      string
	dsn          string
	brokers      string
	topic        string
	leaseTopic   string
	partitions   int
	wait         time.Duration
	minCompleted float64
}

func main() {
	var cfg config
	flag.StringVar(&cfg.results, "results", "results.json", "per-match results written by loadgen")
	flag.StringVar(&cfg.dsn, "dsn", os.Getenv("GAME_POSTGRES_DSN"), "game read model")
	flag.StringVar(&cfg.brokers, "brokers", "kafka:29092", "journal brokers, comma separated")
	flag.StringVar(&cfg.topic, "topic", "game.session-events.v1", "journal topic")
	flag.StringVar(&cfg.leaseTopic, "lease-topic", "game.partition-leases.v1", "lease topic")
	flag.IntVar(&cfg.partitions, "partitions", 24, "journal partitions")
	flag.DurationVar(&cfg.wait, "wait", 2*time.Minute, "how long the projector may take to reach the journal end")
	flag.Float64Var(&cfg.minCompleted, "min-completed", 0.95, "share of matches that must complete")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	report, err := run(ctx, cfg)
	stop()
	if err != nil {
		slog.Error("reconcile failed", "error", err)
		os.Exit(2)
	}
	out, _ := json.MarshalIndent(report, "", " ")
	fmt.Println(string(out))
	problems := report.Problems(cfg.minCompleted)
	for _, problem := range problems {
		slog.Error("reconcile", "problem", problem)
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
	slog.Info("reconciled", "confirmed_moves", report.ConfirmedMoves, "journal_moves", report.JournalMoves, "read_model_moves", report.ReadModelMoves)
}

func run(ctx context.Context, cfg config) (Report, error) {
	raw, err := os.ReadFile(cfg.results)
	if err != nil {
		return Report{}, err
	}
	var results []outcome
	if err := json.Unmarshal(raw, &results); err != nil {
		return Report{}, fmt.Errorf("reconcile: results: %w", err)
	}
	events, err := journal.NewKafka(ctx, journal.KafkaConfig{
		Brokers: strings.Split(cfg.brokers, ","), Topic: cfg.topic, LeaseTopic: cfg.leaseTopic,
		Partitions: int32(cfg.partitions), ReplicationFactor: -1,
	})
	if err != nil {
		return Report{}, err
	}
	defer events.Close()
	conn, err := pgx.Connect(ctx, cfg.dsn)
	if err != nil {
		return Report{}, fmt.Errorf("reconcile: connect read model: %w", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	ends := make([]int64, events.Partitions())
	for partition := range events.Partitions() {
		if ends[partition], err = events.End(ctx, partition); err != nil {
			return Report{}, err
		}
	}
	if err := awaitProjector(ctx, conn, ends, cfg.wait); err != nil {
		return Report{}, err
	}
	logged, err := journalMoves(ctx, events, ends)
	if err != nil {
		return Report{}, err
	}
	model, err := readModel(ctx, conn)
	if err != nil {
		return Report{}, err
	}
	return compare(results, logged, model), nil
}

var errBehind = errors.New("reconcile: projector did not reach the journal end")

func awaitProjector(ctx context.Context, conn *pgx.Conn, ends []int64, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		behind, err := lagging(ctx, conn, ends)
		if err != nil {
			return err
		}
		if behind == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %s", errBehind, behind)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func lagging(ctx context.Context, conn *pgx.Conn, ends []int64) (string, error) {
	rows, err := conn.Query(ctx, `SELECT partition, next_offset FROM game.projector_offsets`)
	if err != nil {
		return "", err
	}
	next := make([]int64, len(ends))
	for rows.Next() {
		var partition int32
		var offset int64
		if err := rows.Scan(&partition, &offset); err != nil {
			rows.Close()
			return "", err
		}
		if int(partition) < len(next) {
			next[partition] = offset
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	for partition, end := range ends {
		if next[partition] < end {
			return fmt.Sprintf("partition %d at %d of %d", partition, next[partition], end), nil
		}
	}
	return "", nil
}

func readModel(ctx context.Context, conn *pgx.Conn) (map[uuid.UUID]projected, error) {
	rows, err := conn.Query(ctx, `SELECT s.id, coalesce(s.state->>'status', ''), coalesce(m.moves, 0)
		FROM game.sessions s
		LEFT JOIN (SELECT session_id, count(*) AS moves FROM game.moves GROUP BY session_id) m ON m.session_id = s.id`)
	if err != nil {
		return nil, fmt.Errorf("reconcile: read model: %w", err)
	}
	defer rows.Close()
	model := map[uuid.UUID]projected{}
	for rows.Next() {
		var id uuid.UUID
		var status string
		var moves int
		if err := rows.Scan(&id, &status, &moves); err != nil {
			return nil, fmt.Errorf("reconcile: read model: %w", err)
		}
		model[id] = projected{Status: engine.Status(status), Moves: moves}
	}
	return model, rows.Err()
}
