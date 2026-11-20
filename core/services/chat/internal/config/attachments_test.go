package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

func TestAttachmentStoreUsesItsOwnMinIOUser(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{
		"MINIO_ROOT_USER":        "root",
		"MINIO_ROOT_PASSWORD":    "root-password",
		"ATTACHMENTS_ACCESS_KEY": "axon-chat",
		"ATTACHMENTS_SECRET_KEY": "scoped-password",
	})

	got := loadAttachments(l)

	if got.AccessKey != "axon-chat" || got.SecretKey != "scoped-password" {
		t.Fatalf("attachment store signs in as %q, want the scoped user", got.AccessKey)
	}
}

func TestAttachmentStoreFallsBackToTheRootUser(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{"MINIO_ROOT_USER": "root", "MINIO_ROOT_PASSWORD": "root-password"})

	got := loadAttachments(l)

	if got.AccessKey != "root" || got.SecretKey != "root-password" {
		t.Fatalf("attachment store signs in as %q", got.AccessKey)
	}
}

func TestProductionRefusesTheDefaultAttachmentSecret(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("POSTGRES_DSN", "postgres://chat_service:s3cr3t-value@postgres:5432/axon")
	t.Setenv("KAFKA_BROKERS", "kafka:29092")
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("REDIS_PASSWORD", "a-real-redis-secret")

	if _, err := Load(); !errors.Is(err, config.ErrUnsafeInProduction) || !strings.Contains(err.Error(), "ATTACHMENTS_SECRET_KEY") {
		t.Fatalf("Load() = %v, want the built-in MinIO password refused", err)
	}

	t.Setenv("ATTACHMENTS_SECRET_KEY", "a-real-generated-secret")
	if _, err := Load(); err != nil {
		t.Fatalf("production refused a real secret: %v", err)
	}
}
