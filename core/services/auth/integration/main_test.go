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
	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/postgres"

	"github.com/bvivg/axon/core/services/auth/internal/avatar"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
)

const (
	superuser    = "axon"
	superpass    = "axon"
	database     = "axon"
	authPassword = "auth"
	chatPassword = "chat"
)

const (
	minioUser     = "axon"
	minioPassword = "axon12345"
	avatarsBucket = "avatars"
)

var pool *postgres.Pool

var (
	avatarPipeline  *avatar.Pipeline
	avatarPublicURL string
)

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

	minioContainer, err := startMinio(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := testcontainers.TerminateContainer(minioContainer); err != nil {
			fmt.Fprintf(os.Stderr, "integration: terminate minio container: %v\n", err)
		}
	}()

	return m.Run(), nil
}

func startMinio(ctx context.Context) (*tcminio.MinioContainer, error) {
	minioContainer, err := tcminio.Run(ctx, "minio/minio:RELEASE.2025-04-22T22-12-26Z",
		tcminio.WithUsername(minioUser),
		tcminio.WithPassword(minioPassword),
	)
	if err != nil {
		return nil, fmt.Errorf("start minio: %w", err)
	}

	endpoint, err := minioContainer.ConnectionString(ctx)
	if err != nil {
		return minioContainer, fmt.Errorf("minio connection string: %w", err)
	}

	client, err := miniogo.New(endpoint, &miniogo.Options{
		Creds: credentials.NewStaticV4(minioUser, minioPassword, ""),
	})
	if err != nil {
		return minioContainer, fmt.Errorf("minio client: %w", err)
	}

	if err := client.MakeBucket(ctx, avatarsBucket, miniogo.MakeBucketOptions{}); err != nil {
		return minioContainer, fmt.Errorf("make bucket: %w", err)
	}

	policy := fmt.Sprintf(`{
		"Version": "2012-10-17",
		"Statement": [{
			"Effect": "Allow",
			"Principal": {"AWS": ["*"]},
			"Action": ["s3:GetObject"],
			"Resource": ["arn:aws:s3:::%s/*"]
		}]
	}`, avatarsBucket)
	if err := client.SetBucketPolicy(ctx, avatarsBucket, policy); err != nil {
		return minioContainer, fmt.Errorf("set bucket policy: %w", err)
	}

	store, err := avatar.NewStore(avatar.StoreConfig{
		Endpoint:  endpoint,
		AccessKey: minioUser,
		SecretKey: minioPassword,
		Bucket:    avatarsBucket,
	})
	if err != nil {
		return minioContainer, fmt.Errorf("avatar store: %w", err)
	}

	avatarPublicURL = "http://" + endpoint
	avatarPipeline = avatar.NewPipeline(store, avatar.NewURLBuilder(avatarPublicURL, avatarsBucket))
	return minioContainer, nil
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
