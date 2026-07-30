package config_test

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

func TestRequiredValuesLoad(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{
		"NAME":    "auth",
		"PORT":    "8080",
		"TIMEOUT": "15m",
	})

	name := l.String("NAME")
	port := l.Int("PORT")
	timeout := l.Duration("TIMEOUT")

	if err := l.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "auth" {
		t.Errorf("NAME = %q, want auth", name)
	}
	if port != 8080 {
		t.Errorf("PORT = %d, want 8080", port)
	}
	if timeout != 15*time.Minute {
		t.Errorf("TIMEOUT = %v, want 15m", timeout)
	}
}

// The whole point of accumulating errors: one startup reports every problem.
func TestAllErrorsReportedAtOnce(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{"PORT": "not-a-number"})

	l.String("DATABASE_URL")
	l.Int("PORT")
	l.Duration("TOKEN_TTL")

	err := l.Err()
	if err == nil {
		t.Fatal("Err() = nil, want three errors")
	}
	for _, want := range []string{"DATABASE_URL", "PORT", "TOKEN_TTL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

func TestMissingRequiredIsErrMissing(t *testing.T) {
	l := config.NewLoaderFromMap(nil)

	l.String("DATABASE_URL")

	if err := l.Err(); !errors.Is(err, config.ErrMissing) {
		t.Fatalf("Err() = %v, want ErrMissing", err)
	}
}

// docker-compose renders an unset variable as an empty string, so empty and
// absent have to mean the same thing.
func TestEmptyValueTreatedAsAbsent(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{"DATABASE_URL": "   "})

	l.String("DATABASE_URL")

	if err := l.Err(); !errors.Is(err, config.ErrMissing) {
		t.Fatalf("Err() = %v, want ErrMissing for a whitespace-only value", err)
	}
}

func TestDefaultsApplyWhenAbsent(t *testing.T) {
	l := config.NewLoaderFromMap(nil)

	addr := l.StringDefault("HTTP_ADDR", ":8080")
	conns := l.IntDefault("MAX_CONNS", 10)
	debug := l.Bool("DEBUG", false)
	ttl := l.DurationDefault("TTL", time.Minute)
	origins := l.StringSlice("CORS_ORIGINS", []string{"http://localhost:3000"})

	if err := l.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != ":8080" || conns != 10 || debug != false || ttl != time.Minute {
		t.Errorf("defaults not applied: %q %d %v %v", addr, conns, debug, ttl)
	}
	if len(origins) != 1 || origins[0] != "http://localhost:3000" {
		t.Errorf("CORS_ORIGINS = %v, want the default", origins)
	}
}

// A typo in an optional variable must surface, not silently become the default.
func TestMalformedOptionalValueIsAnError(t *testing.T) {
	tests := map[string]map[string]string{
		"int":      {"MAX_CONNS": "ten"},
		"bool":     {"DEBUG": "yes-please"},
		"duration": {"TTL": "15 minutes"},
	}

	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			l := config.NewLoaderFromMap(env)

			l.IntDefault("MAX_CONNS", 10)
			l.Bool("DEBUG", false)
			l.DurationDefault("TTL", time.Minute)

			if err := l.Err(); err == nil {
				t.Fatalf("Err() = nil for malformed %s value %v", name, env)
			}
		})
	}
}

func TestStringSliceSplitsAndTrims(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{
		"CORS_ORIGINS": "https://a.example , https://b.example,,https://c.example",
	})

	got := l.StringSlice("CORS_ORIGINS", nil)

	if err := l.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"https://a.example", "https://b.example", "https://c.example"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestOneOf(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{"APP_ENV": "staging"})

	got := l.OneOf("APP_ENV", "development", "development", "test", "production")

	if err := l.Err(); err == nil {
		t.Fatal("Err() = nil for a value outside the allowed set")
	}
	if got != "development" {
		t.Fatalf("OneOf returned %q, want the default on error", got)
	}
}

func TestLoadBaseDefaults(t *testing.T) {
	l := config.NewLoaderFromMap(nil)

	base := config.LoadBase(l, "auth")

	if err := l.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base.Service != "auth" {
		t.Errorf("Service = %q, want auth", base.Service)
	}
	if base.Environment != config.EnvDevelopment {
		t.Errorf("Environment = %q, want development", base.Environment)
	}
	if base.HTTPAddr != ":8080" || base.MetricsAddr != ":9090" {
		t.Errorf("addresses = %q / %q", base.HTTPAddr, base.MetricsAddr)
	}
	if base.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", base.LogLevel)
	}
	if base.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 15s", base.ShutdownTimeout)
	}
	if base.Environment.IsProduction() {
		t.Error("development environment reported as production")
	}
}

func TestLoadBaseRejectsBadLogLevel(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{"LOG_LEVEL": "trace"})

	config.LoadBase(l, "auth")

	err := l.Err()
	if err == nil {
		t.Fatal("Err() = nil for an unknown log level")
	}
	if !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Fatalf("error does not mention LOG_LEVEL: %v", err)
	}
}
