package correlation_test

import (
	"context"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

func TestFromContextEmptyWhenAbsent(t *testing.T) {
	if got := correlation.FromContext(context.Background()); got != "" {
		t.Fatalf("FromContext on bare context = %q, want empty", got)
	}
}

func TestWithIDRoundTrip(t *testing.T) {
	ctx := correlation.WithID(context.Background(), "abc-123")

	if got := correlation.FromContext(ctx); got != "abc-123" {
		t.Fatalf("FromContext = %q, want %q", got, "abc-123")
	}
}

func TestWithIDIgnoresEmpty(t *testing.T) {
	ctx := correlation.WithID(context.Background(), "abc-123")
	ctx = correlation.WithID(ctx, "")

	if got := correlation.FromContext(ctx); got != "abc-123" {
		t.Fatalf("empty id overwrote existing: got %q", got)
	}
}

func TestEnsureGeneratesWhenAbsent(t *testing.T) {
	ctx, id := correlation.Ensure(context.Background())

	if id == "" {
		t.Fatal("Ensure returned an empty id")
	}
	if got := correlation.FromContext(ctx); got != id {
		t.Fatalf("returned id %q not stored in context (got %q)", id, got)
	}
}

func TestEnsurePreservesExisting(t *testing.T) {
	ctx := correlation.WithID(context.Background(), "inbound-id")

	ctx, id := correlation.Ensure(ctx)

	if id != "inbound-id" {
		t.Fatalf("Ensure replaced inbound id: got %q", id)
	}
	if got := correlation.FromContext(ctx); got != "inbound-id" {
		t.Fatalf("context id = %q, want %q", got, "inbound-id")
	}
}

func TestNewIDIsUnique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for range 100 {
		id := correlation.NewID()
		if _, dup := seen[id]; dup {
			t.Fatalf("NewID returned duplicate %q", id)
		}
		seen[id] = struct{}{}
	}
}
