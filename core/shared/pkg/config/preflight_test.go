package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

func productionEnv(extra map[string]string) map[string]string {
	env := map[string]string{
		"APP_ENV":            "production",
		"POSTGRES_DSN":       "postgres://auth_service:9f2c4e1a7b@postgres:5432/axon?sslmode=disable",
		"REDIS_PASSWORD":     "c0ffee-redis-secret",
		"AVATARS_SECRET_KEY": "b3a1d2c4e5f60718",
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

func TestProductionPreflightAcceptsSafeSettings(t *testing.T) {
	l := config.NewLoaderFromMap(productionEnv(map[string]string{
		"REFRESH_COOKIE_SECURE": "true",
		"OAUTH_FAKE_ENABLED":    "false",
		"GAME_TEST_HOOKS":       "false",
		"PPROF_ENABLED":         "false",
	}))

	config.LoadBase(l, "auth")

	if err := l.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProductionPreflightRefusesUnsafeSettings(t *testing.T) {
	cases := map[string]map[string]string{
		"POSTGRES_PASSWORD":      {"POSTGRES_PASSWORD": "axon"},
		"REDIS_PASSWORD":         {"REDIS_PASSWORD": "axon"},
		"EDGE_REDIS_PASSWORD":    {"EDGE_REDIS_PASSWORD": "axon-edge"},
		"MINIO_ROOT_PASSWORD":    {"MINIO_ROOT_PASSWORD": "axon12345"},
		"AVATARS_SECRET_KEY":     {"AVATARS_SECRET_KEY": "axon-avatars-dev-secret"},
		"GAME_INTERNAL_KEY":      {"GAME_INTERNAL_KEY": "axon-dev-game-owner-key-not-for-production"},
		"DEBEZIUM_DB_PASSWORD":   {"DEBEZIUM_DB_PASSWORD": "debezium"},
		"POSTGRES_DSN":           {"POSTGRES_DSN": "postgres://game_service:game@postgres:5432/axon?sslmode=disable"},
		"GAME_READ_DATABASE_URL": {"GAME_READ_DATABASE_URL": "postgres://game_service:game@postgres-replica:5432/axon"},
		"REFRESH_COOKIE_SECURE":  {"REFRESH_COOKIE_SECURE": "false"},
		"OAUTH_FAKE_ENABLED":     {"OAUTH_FAKE_ENABLED": "true"},
		"GAME_TEST_HOOKS":        {"GAME_TEST_HOOKS": "true"},
		"PPROF_ENABLED":          {"PPROF_ENABLED": "true"},
	}
	for key, extra := range cases {
		t.Run(key, func(t *testing.T) {
			l := config.NewLoaderFromMap(productionEnv(extra))

			config.LoadBase(l, "auth")

			err := l.Err()
			if !errors.Is(err, config.ErrUnsafeInProduction) {
				t.Fatalf("Err() = %v, want ErrUnsafeInProduction", err)
			}
			if !strings.Contains(err.Error(), key) {
				t.Fatalf("error does not name %s: %v", key, err)
			}
		})
	}
}

func TestPreflightOnlyRunsInProduction(t *testing.T) {
	for _, env := range []string{"development", "test"} {
		l := config.NewLoaderFromMap(map[string]string{
			"APP_ENV":               env,
			"POSTGRES_PASSWORD":     "axon",
			"MINIO_ROOT_PASSWORD":   "axon12345",
			"OAUTH_FAKE_ENABLED":    "true",
			"GAME_TEST_HOOKS":       "true",
			"REFRESH_COOKIE_SECURE": "false",
		})

		config.LoadBase(l, "auth")

		if err := l.Err(); err != nil {
			t.Fatalf("APP_ENV=%s: unexpected error: %v", env, err)
		}
	}
}

func TestRefuseDefaultSecretCoversBuiltInDefaults(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{"APP_ENV": "production"})
	base := config.LoadBase(l, "chat")

	base.RefuseDefaultSecret(l, "ATTACHMENTS_SECRET_KEY", "axon12345")

	if err := l.Err(); !errors.Is(err, config.ErrUnsafeInProduction) {
		t.Fatalf("Err() = %v, want ErrUnsafeInProduction for a built-in default", err)
	}

	dev := config.NewLoaderFromMap(nil)
	config.LoadBase(dev, "chat").RefuseDefaultSecret(dev, "ATTACHMENTS_SECRET_KEY", "axon12345")
	if err := dev.Err(); err != nil {
		t.Fatalf("development refused a default secret: %v", err)
	}
}
