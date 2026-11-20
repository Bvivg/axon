package outbox_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

func TestSweepDeletesRowsPastTheRetention(t *testing.T) {
	for name, tt := range map[string]struct {
		retention time.Duration
		failure   error
		want      string
		logged    string
	}{
		"default retention": {want: "24h0m0s", logged: "outbox cleaned"},
		"custom retention":  {retention: 2 * time.Hour, want: "2h0m0s", logged: "outbox cleaned"},
		"failure is logged": {failure: errors.New("boom"), want: "24h0m0s", logged: "outbox cleanup failed"},
	} {
		t.Run(name, func(t *testing.T) {
			r := &recorder{err: tt.failure}
			var logs bytes.Buffer
			outbox.Sweeper{DB: r, Table: "chat.outbox", Retention: tt.retention, Logger: slog.New(slog.NewTextHandler(&logs, nil))}.Sweep(context.Background())
			if len(r.calls) != 1 || !strings.HasPrefix(r.calls[0].sql, "DELETE FROM chat.outbox ") || r.calls[0].args[0] != tt.want {
				t.Fatal(r.calls)
			}
			if !strings.Contains(logs.String(), tt.logged) {
				t.Fatalf("logs %q, want %q", logs.String(), tt.logged)
			}
		})
	}
}

func TestRunSweepsRepeatedlyUntilCancelled(t *testing.T) {
	r := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		outbox.Sweeper{DB: r, Table: "auth.outbox", Interval: time.Millisecond}.Run(ctx)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancel")
	}
	if len(r.calls) < 2 {
		t.Fatalf("swept %d times, want at least 2", len(r.calls))
	}
}
