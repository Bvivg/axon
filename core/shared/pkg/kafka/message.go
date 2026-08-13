package kafka

import (
	"context"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

type Headers map[string]string

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

func (h Headers) clone() Headers {
	out := make(Headers, len(h)+1)
	for k, v := range h {
		out[k] = v
	}
	return out
}

type Message struct {
	Topic     Topic
	Key       []byte
	Value     []byte
	Headers   Headers
	Partition int
	Offset    int64
	Time      time.Time
}

func (m Message) CorrelationID() string {
	return m.Headers.Get(correlation.Header)
}

func (m Message) Context(ctx context.Context) (context.Context, string) {
	return correlation.Ensure(correlation.WithID(ctx, m.CorrelationID()))
}

func (m Message) withCorrelation(id string) Headers {
	headers := m.Headers.clone()
	headers[correlation.Header] = id
	return headers
}

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
