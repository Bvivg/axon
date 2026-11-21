package config

import (
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

func TestAvatarStoreUsesItsOwnMinIOUser(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{
		"MINIO_ROOT_USER":     "root",
		"MINIO_ROOT_PASSWORD": "root-password",
		"AVATARS_ACCESS_KEY":  "axon-auth",
		"AVATARS_SECRET_KEY":  "scoped-password",
	})

	got := loadAvatar(l)

	if got.AccessKey != "axon-auth" || got.SecretKey != "scoped-password" {
		t.Fatalf("avatar store signs in as %q, want the scoped user", got.AccessKey)
	}
}

func TestAvatarStoreFallsBackToTheRootUser(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{"MINIO_ROOT_USER": "root", "MINIO_ROOT_PASSWORD": "root-password"})

	got := loadAvatar(l)

	if got.AccessKey != "root" || got.SecretKey != "root-password" {
		t.Fatalf("avatar store signs in as %q", got.AccessKey)
	}
}

func TestProductionRefusesTheDefaultAvatarSecret(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{"APP_ENV": "production"})
	base := config.LoadBase(l, ServiceName)

	refuseDefaultAvatarSecret(l, base, loadAvatar(l))

	if l.Err() == nil {
		t.Fatal("production accepted the built-in MinIO password")
	}
}
