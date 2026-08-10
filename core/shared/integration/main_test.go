//go:build integration

// Package integration exercises the shared transport packages against real
// infrastructure.
//
// It sits between the unit tests, which cover everything provable without a
// broker — topic parsing, header encoding, correlation plumbing, backoff — and
// the services' own e2e suites. What belongs here is what only a real broker
// can show: that a consumer group forms, that offsets are committed exactly
// when the code says they are, and that a correlation ID written into a header
// on one side comes back out of it on the other.
//
//	make test-integration
package integration

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/testcontainers/testcontainers-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"

	"github.com/bvivg/axon/core/shared/pkg/kafka"
)

// brokers is the throwaway cluster every test in this package shares. One
// broker per package rather than per test: the container is the expensive part,
// and tests keep out of each other's way by using their own topics and their
// own consumer groups instead.
var brokers []string

// startupTimeout bounds pulling the image and getting the broker to the point
// where it accepts connections. Pulling on a cold machine is the slow part.
const startupTimeout = 3 * time.Minute

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()

	container, err := tckafka.Run(ctx, "confluentinc/confluent-local:7.5.0",
		tckafka.WithClusterID("axon-test"))
	if err != nil {
		return 0, fmt.Errorf("start kafka: %w", err)
	}
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			fmt.Fprintf(os.Stderr, "integration: terminate container: %v\n", err)
		}
	}()

	brokers, err = container.Brokers(ctx)
	if err != nil {
		return 0, fmt.Errorf("broker addresses: %w", err)
	}

	return m.Run(), nil
}

// topicSeq keeps generated topic names unique inside one run.
var topicSeq atomic.Int64

// newTopic creates a topic and waits until the cluster reports a leader for it.
//
// Topics are created explicitly rather than left to the broker's auto-creation:
// auto-creation is asynchronous, and a consumer that subscribes a moment too
// early sees no partitions and quietly waits forever.
func newTopic(t *testing.T, domain, event string) kafka.Topic {
	t.Helper()

	topic, err := kafka.NewTopic(domain, fmt.Sprintf("%s-%d", event, topicSeq.Add(1)))
	if err != nil {
		t.Fatalf("build topic: %v", err)
	}

	client := &kafkago.Client{Addr: kafkago.TCP(brokers...)}
	resp, err := client.CreateTopics(t.Context(), &kafkago.CreateTopicsRequest{
		Topics: []kafkago.TopicConfig{{
			Topic:             topic.String(),
			NumPartitions:     1,
			ReplicationFactor: 1,
		}},
	})
	if err != nil {
		t.Fatalf("create topic %s: %v", topic, err)
	}
	for name, err := range resp.Errors {
		if err != nil {
			t.Fatalf("create topic %s: %v", name, err)
		}
	}

	waitForLeader(t, client, topic)

	return topic
}

func waitForLeader(t *testing.T, client *kafkago.Client, topic kafka.Topic) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for {
		meta, err := client.Metadata(t.Context(), &kafkago.MetadataRequest{
			Topics: []string{topic.String()},
		})
		if err == nil {
			for _, tp := range meta.Topics {
				if tp.Name != topic.String() || tp.Error != nil {
					continue
				}
				if len(tp.Partitions) > 0 && tp.Partitions[0].Leader.ID >= 0 {
					return
				}
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("topic %s has no leader after 30s (last error: %v)", topic, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
