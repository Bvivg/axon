package kafka_test

import (
	"context"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
	"github.com/bvivg/axon/core/shared/pkg/kafka"
)

func TestHeadersGetIsCaseInsensitive(t *testing.T) {
	headers := kafka.Headers{"x-correlation-id": "abc-123"}

	if got := headers.Get(correlation.Header); got != "abc-123" {
		t.Fatalf("Get(%q) = %q, want abc-123", correlation.Header, got)
	}
}

func TestHeadersGetMissingKey(t *testing.T) {
	headers := kafka.Headers{"content-type": "application/json"}

	if got := headers.Get(correlation.Header); got != "" {
		t.Fatalf("Get on a missing key = %q, want empty", got)
	}
}

func TestHeadersGetOnNilMap(t *testing.T) {
	var headers kafka.Headers

	if got := headers.Get(correlation.Header); got != "" {
		t.Fatalf("Get on a nil map = %q, want empty", got)
	}
}

func TestMessageCorrelationID(t *testing.T) {
	msg := kafka.Message{Headers: kafka.Headers{correlation.Header: "from-header"}}

	if got := msg.CorrelationID(); got != "from-header" {
		t.Fatalf("CorrelationID = %q, want from-header", got)
	}
}

// The consumer's whole reason for existing, as far as observability goes: the
// ID the producer put in the header has to come back out into the context the
// handler runs under, or the trace ends at the bus.
func TestMessageContextLiftsHeaderIntoContext(t *testing.T) {
	msg := kafka.Message{Headers: kafka.Headers{correlation.Header: "from-header"}}

	ctx, id := msg.Context(context.Background())

	if id != "from-header" {
		t.Fatalf("returned id = %q, want from-header", id)
	}
	if got := correlation.FromContext(ctx); got != "from-header" {
		t.Fatalf("context id = %q, want from-header", got)
	}
}

// A message with no header still has to produce a usable ID: for that message
// the consumer is the edge of the system.
func TestMessageContextGeneratesWhenHeaderMissing(t *testing.T) {
	var msg kafka.Message

	ctx, id := msg.Context(context.Background())

	if id == "" {
		t.Fatal("Context returned an empty id for a message without the header")
	}
	if got := correlation.FromContext(ctx); got != id {
		t.Fatalf("context id = %q, want the returned id %q", got, id)
	}
}

// The header wins over an ID that happens to be on the consumer's context:
// the message's own ID is the one that ties it to whoever published it.
func TestMessageContextPrefersHeaderOverContext(t *testing.T) {
	msg := kafka.Message{Headers: kafka.Headers{correlation.Header: "from-header"}}
	ctx := correlation.WithID(context.Background(), "from-context")

	_, id := msg.Context(ctx)

	if id != "from-header" {
		t.Fatalf("id = %q, want from-header", id)
	}
}

func TestMessageContextKeepsContextIDWhenHeaderMissing(t *testing.T) {
	var msg kafka.Message
	ctx := correlation.WithID(context.Background(), "from-context")

	_, id := msg.Context(ctx)

	if id != "from-context" {
		t.Fatalf("id = %q, want from-context", id)
	}
}
