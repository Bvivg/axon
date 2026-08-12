package kafka_test

import (
	"errors"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/kafka"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

func TestNewProducerRejectsMissingBrokers(t *testing.T) {
	_, err := kafka.NewProducer(kafka.ProducerConfig{Service: "chat"}, logger.Discard())

	if !errors.Is(err, kafka.ErrNoBrokers) {
		t.Fatalf("err = %v, want ErrNoBrokers", err)
	}
}

func TestNewProducerRejectsMissingService(t *testing.T) {
	_, err := kafka.NewProducer(kafka.ProducerConfig{Brokers: []string{"broker:9092"}}, logger.Discard())

	if !errors.Is(err, kafka.ErrNoService) {
		t.Fatalf("err = %v, want ErrNoService", err)
	}
}

func TestNewConsumerRejectsMissingTopics(t *testing.T) {
	_, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: []string{"broker:9092"},
		Service: "chat",
	}, logger.Discard())

	if !errors.Is(err, kafka.ErrNoTopics) {
		t.Fatalf("err = %v, want ErrNoTopics", err)
	}
}

func TestNewConsumerRejectsMissingService(t *testing.T) {
	_, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: []string{"broker:9092"},
		Topics:  []kafka.Topic{"chat.message"},
	}, logger.Discard())

	if !errors.Is(err, kafka.ErrNoService) {
		t.Fatalf("err = %v, want ErrNoService", err)
	}
}

func TestNewConsumerRejectsAMalformedTopic(t *testing.T) {
	_, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: []string{"broker:9092"},
		Service: "chat",
		Topics:  []kafka.Topic{"chat.message", "not-a-topic"},
	}, logger.Discard())

	if !errors.Is(err, kafka.ErrInvalidTopic) {
		t.Fatalf("err = %v, want ErrInvalidTopic", err)
	}
}

func TestGroupIDIsTheConsumingService(t *testing.T) {
	cfg := kafka.ConsumerConfig{
		Brokers: []string{"broker:9092"},
		Service: "notifications",
		Topics:  []kafka.Topic{"chat.message"},
	}

	if got := cfg.GroupID(); got != "notifications" {
		t.Fatalf("GroupID = %q, want notifications", got)
	}
}
