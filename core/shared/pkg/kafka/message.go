package kafka

import (
	"context"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

// Headers are a message's metadata. Values are strings because everything Axon
// puts here — correlation IDs, and later a content type or a schema version —
// is text; a header carrying binary payload is a sign the payload belongs in
// Value instead.
type Headers map[string]string

// Get returns the value for key, matching case-insensitively.
//
// Kafka header keys are case-sensitive on the wire, and we always write
// correlation.Header exactly as spelled there. The lookup is lenient anyway:
// a producer we do not own may spell the same header differently, and losing a
// correlation ID to capitalisation is a bad trade.
func (h Headers) Get(key string) string {
	if v, ok := h[key]; ok {
		return v
	}
	for k, v := range h {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// clone copies the map so a caller's map is never mutated by publishing.
func (h Headers) clone() Headers {
	out := make(Headers, len(h)+1)
	for k, v := range h {
		out[k] = v
	}
	return out
}

// Message is one event on the bus.
//
// Key decides partitioning, and partitioning decides ordering: messages with
// the same key land on the same partition and are delivered in order. Chat uses
// the room id, game the session id — ordering matters inside a room or a game
// and nowhere else.
//
// Partition, Offset and Time are filled in by the consumer and ignored by the
// producer.
type Message struct {
	Topic     Topic
	Key       []byte
	Value     []byte
	Headers   Headers
	Partition int
	Offset    int64
	Time      time.Time
}

// CorrelationID returns the correlation ID the message carries, or "" when it
// has none.
func (m Message) CorrelationID() string {
	return m.Headers.Get(correlation.Header)
}

// Context returns ctx carrying the message's correlation ID, and that ID.
//
// A message without the header gets a fresh one: for that message the consumer
// is where the system begins, exactly as the gateway is for an HTTP request, and
// a handler that logs without any ID is worse than one that logs a new one.
func (m Message) Context(ctx context.Context) (context.Context, string) {
	return correlation.Ensure(correlation.WithID(ctx, m.CorrelationID()))
}

// withCorrelation returns a copy of the headers carrying id.
func (m Message) withCorrelation(id string) Headers {
	headers := m.Headers.clone()
	headers[correlation.Header] = id
	return headers
}

// toKafka converts an outbound message, stamping it with the correlation ID.
func (m Message) toKafka(correlationID string) kafkago.Message {
	headers := m.withCorrelation(correlationID)

	out := kafkago.Message{
		Topic:   string(m.Topic),
		Key:     m.Key,
		Value:   m.Value,
		Headers: make([]kafkago.Header, 0, len(headers)),
	}
	for k, v := range headers {
		out.Headers = append(out.Headers, kafkago.Header{Key: k, Value: []byte(v)})
	}
	return out
}

// fromKafka converts an inbound message.
//
// Duplicate header keys keep the first occurrence: Kafka permits repeats, and
// silently preferring the last one would let a later header override a
// correlation ID that a trusted producer set first.
func fromKafka(m kafkago.Message) Message {
	headers := make(Headers, len(m.Headers))
	for _, h := range m.Headers {
		if _, seen := headers[h.Key]; seen {
			continue
		}
		headers[h.Key] = string(h.Value)
	}

	return Message{
		Topic:     Topic(m.Topic),
		Key:       m.Key,
		Value:     m.Value,
		Headers:   headers,
		Partition: m.Partition,
		Offset:    m.Offset,
		Time:      m.Time,
	}
}
