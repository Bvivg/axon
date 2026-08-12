package config_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

const dsn = "postgres://user:sup3rs3cret@db:5432/axon"

func TestSecretHiddenFromFormatting(t *testing.T) {
	s := config.NewSecret(dsn)

	for _, format := range []string{"%s", "%v", "%q", "%#v", "%+v"} {
		got := fmt.Sprintf(format, s)
		if strings.Contains(got, "sup3rs3cret") {
			t.Errorf("%s leaked the secret: %s", format, got)
		}
	}
}

func TestSecretHiddenInsideStruct(t *testing.T) {
	cfg := struct {
		Service string
		DSN     config.Secret
	}{Service: "auth", DSN: config.NewSecret(dsn)}

	if got := fmt.Sprintf("%+v", cfg); strings.Contains(got, "sup3rs3cret") {
		t.Errorf("struct formatting leaked the secret: %s", got)
	}

	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if bytes.Contains(encoded, []byte("sup3rs3cret")) {
		t.Errorf("JSON encoding leaked the secret: %s", encoded)
	}
}

func TestSecretHiddenInLogs(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	log.Info("configuration loaded", "dsn", config.NewSecret(dsn))

	if strings.Contains(buf.String(), "sup3rs3cret") {
		t.Errorf("log line leaked the secret: %s", buf.String())
	}
}

func TestSecretRevealReturnsValue(t *testing.T) {
	if got := config.NewSecret(dsn).Reveal(); got != dsn {
		t.Fatalf("Reveal = %q, want the original value", got)
	}
}

func TestSecretIsZero(t *testing.T) {
	if !config.NewSecret("").IsZero() {
		t.Error("empty secret reported as non-zero")
	}
	if config.NewSecret("x").IsZero() {
		t.Error("non-empty secret reported as zero")
	}
}

func TestLoaderSecret(t *testing.T) {
	l := config.NewLoaderFromMap(map[string]string{"DATABASE_URL": dsn})

	got := l.Secret("DATABASE_URL")

	if err := l.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Reveal() != dsn {
		t.Fatalf("Reveal = %q, want the original value", got.Reveal())
	}
}

func TestLoaderSecretMissing(t *testing.T) {
	l := config.NewLoaderFromMap(nil)

	got := l.Secret("JWT_PRIVATE_KEY")

	if err := l.Err(); err == nil {
		t.Fatal("Err() = nil for a missing secret")
	}
	if !got.IsZero() {
		t.Fatalf("missing secret is not zero: %q", got.Reveal())
	}
}
