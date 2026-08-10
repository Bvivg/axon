package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

// Encoding a message is where the correlation ID leaves the process, so it is
// worth testing on its own rather than only through a broker.
func TestToKafkaStampsCorrelationID(t *testing.T) {
	msg := Message{
		Topic:   "chat.message",
		Key:     []byte("room-1"),
		Value:   []byte(`{"text":"hi"}`),
		Headers: Headers{"content-type": "application/json"},
	}

	out := msg.toKafka("trace-1")

	if out.Topic != "chat.message" {
		t.Fatalf("topic = %q, want chat.message", out.Topic)
	}
	if string(out.Key) != "room-1" || string(out.Value) != `{"text":"hi"}` {
		t.Fatalf("key/value did not survive: %q/%q", out.Key, out.Value)
	}

	got := headerValue(out.Headers, correlation.Header)
	if got != "trace-1" {
		t.Fatalf("%s = %q, want trace-1", correlation.Header, got)
	}
	if got := headerValue(out.Headers, "content-type"); got != "application/json" {
		t.Fatalf("caller header lost: content-type = %q", got)
	}
}

// Publishing must not edit the caller's map: a caller that reuses one Headers
// value for several messages would otherwise carry the first message's
// correlation ID into all of them.
func TestToKafkaDoesNotMutateCallerHeaders(t *testing.T) {
	headers := Headers{"content-type": "application/json"}
	msg := Message{Topic: "chat.message", Headers: headers}

	msg.toKafka("trace-1")

	if _, found := headers[correlation.Header]; found {
		t.Fatal("toKafka wrote the correlation ID into the caller's map")
	}
	if len(headers) != 1 {
		t.Fatalf("caller headers changed size: %v", headers)
	}
}

func TestToKafkaOverwritesAStaleCorrelationHeader(t *testing.T) {
	msg := Message{
		Topic:   "chat.message",
		Headers: Headers{correlation.Header: "stale"},
	}

	out := msg.toKafka("trace-1")

	if got := headerValue(out.Headers, correlation.Header); got != "trace-1" {
		t.Fatalf("%s = %q, want the publishing context's trace-1", correlation.Header, got)
	}
}

func TestFromKafkaDecodesTheMessage(t *testing.T) {
	when := time.Now().Truncate(time.Millisecond)
	raw := kafkago.Message{
		Topic:     "game.started",
		Partition: 3,
		Offset:    17,
		Key:       []byte("session-9"),
		Value:     []byte("payload"),
		Time:      when,
		Headers: []kafkago.Header{
			{Key: correlation.Header, Value: []byte("trace-1")},
		},
	}

	msg := fromKafka(raw)

	if msg.Topic != Topic("game.started") {
		t.Fatalf("topic = %q", msg.Topic)
	}
	if msg.Partition != 3 || msg.Offset != 17 {
		t.Fatalf("partition/offset = %d/%d, want 3/17", msg.Partition, msg.Offset)
	}
	if string(msg.Key) != "session-9" || string(msg.Value) != "payload" {
		t.Fatalf("key/value = %q/%q", msg.Key, msg.Value)
	}
	if !msg.Time.Equal(when) {
		t.Fatalf("time = %v, want %v", msg.Time, when)
	}
	if msg.CorrelationID() != "trace-1" {
		t.Fatalf("correlation id = %q, want trace-1", msg.CorrelationID())
	}
}

// Kafka allows repeated header keys. Preferring the first one means a later
// duplicate cannot overwrite the correlation ID a trusted producer set.
func TestFromKafkaKeepsTheFirstDuplicateHeader(t *testing.T) {
	raw := kafkago.Message{
		Topic: "chat.message",
		Headers: []kafkago.Header{
			{Key: correlation.Header, Value: []byte("first")},
			{Key: correlation.Header, Value: []byte("second")},
		},
	}

	if got := fromKafka(raw).CorrelationID(); got != "first" {
		t.Fatalf("correlation id = %q, want first", got)
	}
}

// The whole path in one test: a context with an ID, encoded onto the wire,
// decoded on the other side, and lifted back into a handler's context.
func TestCorrelationSurvivesTheRoundTrip(t *testing.T) {
	ctx := correlation.WithID(context.Background(), "trace-1")
	_, id := correlation.Ensure(ctx)

	out := Message{Topic: "chat.message", Value: []byte("x")}.toKafka(id)
	in := fromKafka(kafkago.Message{
		Topic:   out.Topic,
		Key:     out.Key,
		Value:   out.Value,
		Headers: out.Headers,
	})

	handlerCtx, handlerID := in.Context(context.Background())

	if handlerID != "trace-1" {
		t.Fatalf("handler id = %q, want trace-1", handlerID)
	}
	if got := correlation.FromContext(handlerCtx); got != "trace-1" {
		t.Fatalf("handler context id = %q, want trace-1", got)
	}
}

func TestRunWithoutHandler(t *testing.T) {
	// A zero Consumer is enough: the check happens before anything touches the
	// broker, which is exactly the guarantee being tested.
	err := (&Consumer{}).Run(context.Background(), nil)

	if !errors.Is(err, ErrNoHandler) {
		t.Fatalf("Run(nil) = %v, want ErrNoHandler", err)
	}
}

func headerValue(headers []kafkago.Header, key string) string {
	for _, h := range headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}
