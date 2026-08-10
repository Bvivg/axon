//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
	"github.com/bvivg/axon/core/shared/pkg/kafka"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

// consumeTimeout bounds a consumer that is waiting for messages. It is
// generous because forming a consumer group involves a join and a rebalance,
// which take longer than reading the messages afterwards.
const consumeTimeout = 60 * time.Second

// handled is one message as its handler saw it, including the correlation ID
// that was on the context — the part that cannot be checked after the fact.
type handled struct {
	msg           kafka.Message
	correlationID string
}

func newProducer(t *testing.T, service string) *kafka.Producer {
	t.Helper()

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers: brokers,
		Service: service,
	}, logger.Discard())
	if err != nil {
		t.Fatalf("new producer: %v", err)
	}
	t.Cleanup(func() {
		if err := producer.Close(); err != nil {
			t.Errorf("close producer: %v", err)
		}
	})

	return producer
}

// consumeN runs a consumer until it has handled n messages, then stops it.
func consumeN(t *testing.T, service string, topic kafka.Topic, n int) []handled {
	t.Helper()

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: brokers,
		Service: service,
		Topics:  []kafka.Topic{topic},
	}, logger.Discard())
	if err != nil {
		t.Fatalf("new consumer: %v", err)
	}
	defer func() {
		if err := consumer.Close(); err != nil {
			t.Errorf("close consumer: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), consumeTimeout)
	defer cancel()

	var got []handled
	err = consumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
		got = append(got, handled{msg: msg, correlationID: correlation.FromContext(ctx)})
		if len(got) == n {
			cancel()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("run consumer: %v", err)
	}
	if len(got) != n {
		t.Fatalf("handled %d messages, want %d", len(got), n)
	}

	return got
}

// The first consumer this package was designed for: chat publishes
// chat.message, and a service that cares about it reads the message with its
// key, its payload and its headers intact.
func TestPublishAndConsume(t *testing.T) {
	topic := newTopic(t, "chat", "message")
	producer := newProducer(t, "chat")

	ctx := correlation.WithID(t.Context(), "trace-round-trip")
	err := producer.Publish(ctx, kafka.Message{
		Topic:   topic,
		Key:     []byte("room-1"),
		Value:   []byte(`{"text":"hello"}`),
		Headers: kafka.Headers{"content-type": "application/json"},
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	got := consumeN(t, "notifications", topic, 1)[0]

	if got.msg.Topic != topic {
		t.Errorf("topic = %q, want %q", got.msg.Topic, topic)
	}
	if string(got.msg.Key) != "room-1" {
		t.Errorf("key = %q, want room-1", got.msg.Key)
	}
	if string(got.msg.Value) != `{"text":"hello"}` {
		t.Errorf("value = %q", got.msg.Value)
	}
	if ct := got.msg.Headers.Get("content-type"); ct != "application/json" {
		t.Errorf("content-type header = %q", ct)
	}
}

// rules/observability.md wants one ID to follow a request through every service
// that touches it. Kafka is where that chain is easiest to drop, since nothing
// carries a context across the bus but the headers we put there ourselves.
func TestCorrelationIDCrossesTheBus(t *testing.T) {
	topic := newTopic(t, "game", "started")
	producer := newProducer(t, "game")

	ctx := correlation.WithID(t.Context(), "trace-across-the-bus")
	if err := producer.Publish(ctx, kafka.Message{Topic: topic, Value: []byte("session-1")}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got := consumeN(t, "analytics", topic, 1)[0]

	if got.msg.CorrelationID() != "trace-across-the-bus" {
		t.Errorf("header id = %q, want trace-across-the-bus", got.msg.CorrelationID())
	}
	if got.correlationID != "trace-across-the-bus" {
		t.Errorf("handler context id = %q, want trace-across-the-bus", got.correlationID)
	}
}

// Publishing from a context that never had an ID must not produce a message
// with no ID at all: the publish and everything the consumer does with it still
// have to share one.
func TestPublishWithoutACorrelationIDStampsOne(t *testing.T) {
	topic := newTopic(t, "game", "finished")
	producer := newProducer(t, "game")

	if err := producer.Publish(t.Context(), kafka.Message{Topic: topic, Value: []byte("x")}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got := consumeN(t, "analytics", topic, 1)[0]

	if got.msg.CorrelationID() == "" {
		t.Error("message crossed the bus with no correlation ID")
	}
	if got.correlationID != got.msg.CorrelationID() {
		t.Errorf("handler context id = %q, want the header's %q", got.correlationID, got.msg.CorrelationID())
	}
}

// A message from a producer that is not ours — an operator's console, another
// team's service — has no header. The consumer is that message's edge of the
// system, so it starts a new trace rather than logging without one.
func TestMessageWithoutTheHeaderGetsAFreshID(t *testing.T) {
	topic := newTopic(t, "chat", "message")

	writer := &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Topic:        topic.String(),
		RequiredAcks: kafkago.RequireAll,
		BatchTimeout: 10 * time.Millisecond,
	}
	if err := writer.WriteMessages(t.Context(), kafkago.Message{Value: []byte("headerless")}); err != nil {
		t.Fatalf("write raw message: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close raw writer: %v", err)
	}

	got := consumeN(t, "notifications", topic, 1)[0]

	if got.msg.CorrelationID() != "" {
		t.Fatalf("the raw message carried a correlation ID: %q", got.msg.CorrelationID())
	}
	if got.correlationID == "" {
		t.Error("handler ran with no correlation ID at all")
	}
}

// The offset moves only after the handler succeeds, so a consumer that comes
// back does not redo work it already reported as done.
func TestCommittedOffsetsAreNotRedelivered(t *testing.T) {
	topic := newTopic(t, "game", "started")
	producer := newProducer(t, "game")

	for _, payload := range []string{"first", "second"} {
		if err := producer.Publish(t.Context(), kafka.Message{Topic: topic, Value: []byte(payload)}); err != nil {
			t.Fatalf("publish %s: %v", payload, err)
		}
	}

	first := consumeN(t, "leaderboard", topic, 2)
	if string(first[0].msg.Value) != "first" || string(first[1].msg.Value) != "second" {
		t.Fatalf("messages arrived out of order: %q, %q", first[0].msg.Value, first[1].msg.Value)
	}

	// Same group again: everything it handled is committed, so it should idle.
	if got := drain(t, "leaderboard", topic, 5*time.Second); len(got) != 0 {
		t.Fatalf("the group was handed %d already-committed messages", len(got))
	}

	// A different service reads the topic from the start — proof that the
	// silence above was committed offsets and not an empty topic.
	if got := consumeN(t, "analytics", topic, 2); len(got) != 2 {
		t.Fatalf("a fresh group saw %d messages, want 2", len(got))
	}
}

// A handler that fails stops the consumer and leaves the offset alone, so the
// message is still there afterwards. The alternative — carry on — would drop it
// silently, because the next commit jumps over the offset that failed.
func TestFailedHandlerLeavesTheMessageUncommitted(t *testing.T) {
	topic := newTopic(t, "chat", "message")
	producer := newProducer(t, "chat")

	if err := producer.Publish(t.Context(), kafka.Message{Topic: topic, Value: []byte("poison")}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: brokers,
		Service: "notifications",
		Topics:  []kafka.Topic{topic},
	}, logger.Discard())
	if err != nil {
		t.Fatalf("new consumer: %v", err)
	}

	handlerErr := errors.New("handler said no")
	ctx, cancel := context.WithTimeout(t.Context(), consumeTimeout)
	defer cancel()

	runErr := consumer.Run(ctx, func(_ context.Context, _ kafka.Message) error {
		return handlerErr
	})
	if !errors.Is(runErr, handlerErr) {
		t.Fatalf("Run = %v, want the handler's error", runErr)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("close consumer: %v", err)
	}

	// The same group starts again and gets the message back.
	again := consumeN(t, "notifications", topic, 1)[0]
	if string(again.msg.Value) != "poison" {
		t.Fatalf("redelivered %q, want poison", again.msg.Value)
	}
}

// Readiness has to fail while the brokers are unreachable, which is what keeps
// an unready replica out of rotation instead of failing requests.
func TestHealthCheck(t *testing.T) {
	producer := newProducer(t, "chat")

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	if err := producer.Check(ctx); err != nil {
		t.Fatalf("Check against a live broker = %v, want nil", err)
	}
	if producer.Name() != "kafka" {
		t.Errorf("Name = %q, want kafka", producer.Name())
	}

	unreachable, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers: []string{"127.0.0.1:1"},
		Service: "chat",
	}, logger.Discard())
	if err != nil {
		t.Fatalf("new producer: %v", err)
	}
	defer func() { _ = unreachable.Close() }()

	deadCtx, deadCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer deadCancel()

	if err := unreachable.Check(deadCtx); err == nil {
		t.Error("Check against a dead broker returned nil")
	}
}

// drain consumes for a fixed window and returns whatever showed up, which may
// be nothing. Used where the expected answer is "no messages".
func drain(t *testing.T, service string, topic kafka.Topic, window time.Duration) []handled {
	t.Helper()

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: brokers,
		Service: service,
		Topics:  []kafka.Topic{topic},
	}, logger.Discard())
	if err != nil {
		t.Fatalf("new consumer: %v", err)
	}
	defer func() {
		if err := consumer.Close(); err != nil {
			t.Errorf("close consumer: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), window)
	defer cancel()

	var got []handled
	if err := consumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
		got = append(got, handled{msg: msg, correlationID: correlation.FromContext(ctx)})
		return nil
	}); err != nil {
		t.Fatalf("run consumer: %v", err)
	}

	return got
}
