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

func TestValid(t *testing.T) {
	for id, want := range map[string]bool{
		"6f1c2a4e-9b7d-4c3a-8e21-0d5f7a9b3c11":          true,
		"6F1C2A4E-9B7D-4C3A-8E21-0D5F7A9B3C11":          true,
		"01HZX3F5Q8K2V7N9M4T6B1C0DE":                    true,
		correlation.NewID():                             true,
		"":                                              false,
		"corr-99":                                       false,
		"6f1c2a4e9b7d4c3a8e210d5f7a9b3c11":              false,
		"{6f1c2a4e-9b7d-4c3a-8e21-0d5f7a9b3c11}":        false,
		"urn:uuid:6f1c2a4e-9b7d-4c3a-8e21-0d5f7a9b3c11": false,
		"6f1c2a4e-9b7d-4c3a-8e21-0d5f7a9b3c1\n":         false,
		"01HZX3F5Q8K2V7N9M4T6B1C0DU":                    false,
		"81HZX3F5Q8K2V7N9M4T6B1C0DE":                    false,
	} {
		if got := correlation.Valid(id); got != want {
			t.Errorf("Valid(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestFromHeaderDropsAnInvalidID(t *testing.T) {
	if got := correlation.FromHeader("6f1c2a4e-9b7d-4c3a-8e21-0d5f7a9b3c11"); got != "6f1c2a4e-9b7d-4c3a-8e21-0d5f7a9b3c11" {
		t.Fatalf("a valid id was dropped: %q", got)
	}
	if got := correlation.FromHeader("free text"); got != "" {
		t.Fatalf("an invalid id passed: %q", got)
	}
}
