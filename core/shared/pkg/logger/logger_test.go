package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line is not valid JSON (%v): %s", err, buf.String())
	}
	return rec
}

func TestMandatoryFields(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Options{Service: "auth", Output: &buf})

	log.InfoContext(context.Background(), "service started")

	rec := decode(t, &buf)
	for _, field := range []string{logger.FieldTimestamp, logger.FieldLevel, logger.FieldService} {
		if _, ok := rec[field]; !ok {
			t.Errorf("missing mandatory field %q in %v", field, rec)
		}
	}
	if rec[logger.FieldService] != "auth" {
		t.Errorf("service = %v, want auth", rec[logger.FieldService])
	}
	if rec["msg"] != "service started" {
		t.Errorf("msg = %v, want %q", rec["msg"], "service started")
	}
	if _, ok := rec["time"]; ok {
		t.Error(`slog's default "time" key was not renamed to "timestamp"`)
	}
}

func TestCorrelationIDPulledFromContext(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Options{Service: "gateway", Output: &buf})

	ctx := correlation.WithID(context.Background(), "corr-42")
	log.InfoContext(ctx, "handled request")

	rec := decode(t, &buf)
	if rec[logger.FieldCorrelationID] != "corr-42" {
		t.Fatalf("correlation_id = %v, want corr-42", rec[logger.FieldCorrelationID])
	}
}

func TestCorrelationIDAbsentWhenContextHasNone(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Options{Service: "gateway", Output: &buf})

	log.InfoContext(context.Background(), "background job")

	rec := decode(t, &buf)
	if _, ok := rec[logger.FieldCorrelationID]; ok {
		t.Fatalf("correlation_id present without one on the context: %v", rec)
	}
}

func TestCorrelationIDSurvivesWith(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Options{Service: "chat", Output: &buf}).With("room_id", "r1")

	ctx := correlation.WithID(context.Background(), "corr-7")
	log.InfoContext(ctx, "message sent")

	rec := decode(t, &buf)
	if rec[logger.FieldCorrelationID] != "corr-7" {
		t.Fatalf("correlation_id lost after With: %v", rec)
	}
	if rec["room_id"] != "r1" {
		t.Fatalf("room_id = %v, want r1", rec["room_id"])
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Options{Service: "auth", Level: slog.LevelWarn, Output: &buf})

	log.InfoContext(context.Background(), "should be dropped")
	if buf.Len() != 0 {
		t.Fatalf("info line emitted at warn level: %s", buf.String())
	}

	log.WarnContext(context.Background(), "should be kept")
	if buf.Len() == 0 {
		t.Fatal("warn line dropped at warn level")
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{in: "debug", want: slog.LevelDebug},
		{in: "info", want: slog.LevelInfo},
		{in: "", want: slog.LevelInfo},
		{in: "  WARN ", want: slog.LevelWarn},
		{in: "warning", want: slog.LevelWarn},
		{in: "Error", want: slog.LevelError},
		{in: "trace", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := logger.ParseLevel(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q): unexpected error %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseLevel(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		in      string
		want    logger.Format
		wantErr bool
	}{
		{in: "json", want: logger.FormatJSON},
		{in: "", want: logger.FormatJSON},
		{in: "TEXT", want: logger.FormatText},
		{in: "logfmt", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := logger.ParseFormat(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseFormat(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFormat(%q): unexpected error %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseFormat(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
