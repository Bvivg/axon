//go:build integration

// Package integration exercises the chat repository against a real Postgres.
//
// It sits between the unit tests, which cover pure logic with no database at
// all, and the e2e suite, which drives the whole stack through the gateway.
// What belongs here is everything that is genuinely about the database and
// invisible from either side: constraint behaviour, transaction boundaries,
// concurrency, and the permission model.
//
// The concurrency tests are the reason this package exists at all. Message
// ordering is a claim about what two transactions can do to each other, and
// nothing short of two real transactions can check it.
//
//	make test-integration
package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
	"github.com/bvivg/axon/core/shared/pkg/redis"

	"github.com/bvivg/axon/core/services/chat/internal/repository"
)

// Credentials for the throwaway container. They exist only inside a container
// that lives for the length of one test run.
const (
	superuser    = "axon"
	superpass    = "axon"
	database     = "axon"
	authPassword = "auth"
	chatPassword = "chat"
)

// pool is the connection the tests use: the chat_service role, not the
// superuser. Testing through the role the service actually uses is the only way
// the permission model is under test rather than merely configured.
var pool *postgres.Pool

// cache is the Redis the fan-out tests use. A real one rather than an in-memory
// stand-in: what those tests are about is two processes agreeing through a
// broker, and a fake would only agree with itself.
var cache *redis.Client

// startupTimeout bounds bringing the container up and migrating it. Pulling the
// image on a cold machine is the slow part.
const startupTimeout = 3 * time.Minute

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

	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase(database),
		tcpostgres.WithUsername(superuser),
		tcpostgres.WithPassword(superpass),
		// The same script the real stack runs. Reusing it is the point: a test
		// against a hand-rolled schema would prove nothing about the permissions
		// the deployed database actually grants.
		tcpostgres.WithInitScripts("../../../deploy/postgres/init/01-schemas.sh"),
		testcontainers.WithEnv(map[string]string{
			"AUTH_DB_PASSWORD":    authPassword,
			"CHAT_DB_PASSWORD":    chatPassword,
			"GAME_DB_PASSWORD":    "game",
			"CALLING_DB_PASSWORD": "calling",
		}),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Minute),
		),
	)
	if err != nil {
		return 0, fmt.Errorf("start postgres: %w", err)
	}
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			fmt.Fprintf(os.Stderr, "integration: terminate container: %v\n", err)
		}
	}()

	host, err := container.Host(ctx)
	if err != nil {
		return 0, fmt.Errorf("container host: %w", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return 0, fmt.Errorf("container port: %w", err)
	}

	chatDSN := fmt.Sprintf("postgres://chat_service:%s@%s:%s/%s?sslmode=disable",
		chatPassword, host, port.Port(), database)

	if err := applyMigrations(chatDSN); err != nil {
		return 0, err
	}

	pool, err = postgres.Connect(ctx, postgres.Config{
		DSN:        config.NewSecret(chatDSN),
		SearchPath: "chat",
	}, logger.Discard())
	if err != nil {
		return 0, fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	redisContainer, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		return 0, fmt.Errorf("start redis: %w", err)
	}
	defer func() {
		if err := testcontainers.TerminateContainer(redisContainer); err != nil {
			fmt.Fprintf(os.Stderr, "integration: terminate redis: %v\n", err)
		}
	}()

	redisAddr, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		return 0, fmt.Errorf("redis address: %w", err)
	}

	cache, err = redis.Connect(ctx, redis.Config{
		Addr: strings.TrimPrefix(redisAddr, "redis://"),
	}, logger.Discard())
	if err != nil {
		return 0, fmt.Errorf("connect redis: %w", err)
	}
	defer func() {
		if err := cache.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "integration: close redis: %v\n", err)
		}
	}()

	return m.Run(), nil
}

// applyMigrations runs the service's own migrations, with the service's own
// role. It doubles as a check that they apply to an empty database at all.
func applyMigrations(dsn string) error {
	m, err := migrate.New("file://../migrations", "pgx5://"+dsn[len("postgres://"):])
	if err != nil {
		return fmt.Errorf("open migrations: %w", err)
	}
	defer func() {
		sourceErr, dbErr := m.Close()
		if sourceErr != nil || dbErr != nil {
			fmt.Fprintf(os.Stderr, "integration: close migrate: %v %v\n", sourceErr, dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// newRepo returns a repository over a database with no rows in it.
//
// Truncating between tests rather than starting a container per test keeps the
// suite to one container: the container is the expensive part, and an empty
// table is an empty table however it got that way.
func newRepo(t *testing.T) *repository.Repository {
	t.Helper()

	// CASCADE because messages and room_members both reference rooms. The
	// per-room counter lives on the room row, so truncating rooms resets it —
	// there is no sequence to restart.
	if _, err := pool.Exec(t.Context(), `TRUNCATE rooms, room_members, messages CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return repository.New(pool)
}
