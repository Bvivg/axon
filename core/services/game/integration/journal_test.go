//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/bvivg/axon/core/services/game/internal/journal"
)

func TestJournalTopicIsKeptForever(t *testing.T) {
	ctx := context.Background()
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	admin := kadm.NewClient(client)
	suffix := uuid.NewString()[:8]
	fresh, defaults := "game.session-events.fresh-"+suffix, "game.session-events.defaults-"+suffix
	if _, err := admin.CreateTopic(ctx, partitions, 1, nil, defaults); err != nil {
		t.Fatal(err)
	}
	for _, topic := range []string{fresh, defaults} {
		log, err := journal.NewKafka(ctx, journal.KafkaConfig{Brokers: brokers, Topic: topic, LeaseTopic: topic + ".leases", Partitions: partitions, ReplicationFactor: 1})
		if err != nil {
			t.Fatal(topic, err)
		}
		log.Close()
		described, err := admin.DescribeTopicConfigs(ctx, topic)
		if err != nil {
			t.Fatal(err)
		}
		configs, err := described.On(topic, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, config := range configs.Configs {
			if config.Value != nil {
				got[config.Key] = *config.Value
			}
		}
		if got["retention.ms"] != "-1" || got["cleanup.policy"] != "delete" {
			t.Fatal(topic, "journal retention", got["retention.ms"], got["cleanup.policy"])
		}
	}
}
