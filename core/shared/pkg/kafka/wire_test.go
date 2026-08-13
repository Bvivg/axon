package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

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
