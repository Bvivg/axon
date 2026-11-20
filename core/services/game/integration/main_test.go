//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const startupTimeout = 3 * time.Minute

var (
	pool    *pgxpool.Pool
	brokers []string
)

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	db, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("axon"),
		tcpostgres.WithUsername("axon"),
		tcpostgres.WithPassword("axon"),
		tcpostgres.WithInitScripts("../../../deploy/postgres/init/01-schemas.sh", "../../../deploy/postgres/init/02-extensions.sql"),
		testcontainers.WithEnv(map[string]string{"AUTH_DB_PASSWORD": "auth", "CHAT_DB_PASSWORD": "chat", "GAME_DB_PASSWORD": "game", "CALLING_DB_PASSWORD": "calling", "RATING_DB_PASSWORD": "rating"}),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(time.Minute)),
	)
	if err != nil {
		return 0, fmt.Errorf("start postgres: %w", err)
	}
	defer func() { _ = testcontainers.TerminateContainer(db) }()
	broker, err := tckafka.Run(ctx, "confluentinc/confluent-local:7.5.0", tckafka.WithClusterID("axon-game-test"))
	if err != nil {
		return 0, fmt.Errorf("start kafka: %w", err)
	}
	defer func() { _ = testcontainers.TerminateContainer(broker) }()
	if brokers, err = broker.Brokers(ctx); err != nil {
		return 0, fmt.Errorf("broker addresses: %w", err)
	}
	host, err := db.Host(ctx)
	if err != nil {
		return 0, err
	}
	port, err := db.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return 0, err
	}
	dsn := fmt.Sprintf("postgres://game_service:game@%s:%s/axon?sslmode=disable&search_path=game", host, port.Port())
	migrator, err := migrate.New("file://../migrations", "pgx5://"+dsn[len("postgres://"):])
	if err != nil {
		return 0, fmt.Errorf("migrations: %w", err)
	}
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 0, fmt.Errorf("migrate: %w", err)
	}
	if pool, err = pgxpool.New(ctx, dsn); err != nil {
		return 0, err
	}
	defer pool.Close()
	return m.Run(), nil
}
