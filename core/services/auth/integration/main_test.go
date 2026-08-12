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
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/postgres"

	"github.com/bvivg/axon/core/services/auth/internal/repository"
)

const (
	superuser    = "axon"
	superpass    = "axon"
	database     = "axon"
	authPassword = "auth"
	chatPassword = "chat"
)

var pool *postgres.Pool

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

	authDSN := fmt.Sprintf("postgres://auth_service:%s@%s:%s/%s?sslmode=disable",
		authPassword, host, port.Port(), database)

	if err := applyMigrations(authDSN); err != nil {
		return 0, err
	}

	pool, err = postgres.Connect(ctx, postgres.Config{
		DSN:        config.NewSecret(authDSN),
		SearchPath: "auth",
	}, logger.Discard())
	if err != nil {
		return 0, fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	return m.Run(), nil
}

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

func newRepo(t *testing.T) *repository.Repository {
	t.Helper()

	_, err := pool.Exec(t.Context(),
		`TRUNCATE users, credentials, refresh_tokens, oauth_accounts CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return repository.New(pool)
}
