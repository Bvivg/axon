package journal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

type KafkaConfig struct {
	Brokers           []string
	Topic             string
	LeaseTopic        string
	Partitions        int32
	ReplicationFactor int16
	MinInSync         int
}

type Kafka struct {
	cfg      KafkaConfig
	producer *kgo.Client
	admin    *kadm.Client
}

func NewKafka(ctx context.Context, cfg KafkaConfig) (*Kafka, error) {
	if len(cfg.Brokers) == 0 || cfg.Topic == "" || cfg.LeaseTopic == "" || cfg.Partitions <= 0 {
		return nil, errors.New("journal: brokers, topics and partitions are required")
	}
	producer, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.DefaultProduceTopic(cfg.Topic),
		kgo.RecordPartitioner(kgo.ManualPartitioner()),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerLinger(0),
		kgo.ProduceRequestTimeout(10*time.Second),
		kgo.RecordDeliveryTimeout(15*time.Second),
	)
	if err != nil {
		return nil, err
	}
	k := &Kafka{cfg: cfg, producer: producer, admin: kadm.NewClient(producer)}
	if err := k.ensureTopics(ctx); err != nil {
		producer.Close()
		return nil, err
	}
	return k, nil
}

func (k *Kafka) ensureTopics(ctx context.Context) error {
	configs := map[string]*string{}
	if k.cfg.MinInSync > 0 {
		value := fmt.Sprint(k.cfg.MinInSync)
		configs["min.insync.replicas"] = &value
	}
	for _, topic := range []string{k.cfg.Topic, k.cfg.LeaseTopic} {
		if _, err := k.admin.CreateTopic(ctx, k.cfg.Partitions, k.cfg.ReplicationFactor, configs, topic); err != nil && !errors.Is(err, kerr.TopicAlreadyExists) {
			return fmt.Errorf("journal: create topic %s: %w", topic, err)
		}
	}
	details, err := k.admin.ListTopics(ctx, k.cfg.Topic)
	if err != nil {
		return err
	}
	if got := len(details[k.cfg.Topic].Partitions); got != int(k.cfg.Partitions) {
		return fmt.Errorf("journal: topic %s has %d partitions, want %d", k.cfg.Topic, got, k.cfg.Partitions)
	}
	return nil
}

func (k *Kafka) Config() KafkaConfig { return k.cfg }

func (k *Kafka) Close() { k.producer.Close() }

func (k *Kafka) Partitions() int32 { return k.cfg.Partitions }

func (k *Kafka) Name() string { return "kafka" }

func (k *Kafka) Check(ctx context.Context) error { return k.producer.Ping(ctx) }

func (k *Kafka) Append(ctx context.Context, partition int32, entry Entry) (int64, error) {
	raw, err := Encode(entry)
	if err != nil {
		return 0, err
	}
	record := &kgo.Record{Topic: k.cfg.Topic, Partition: partition, Value: raw}
	if entry.Event != nil {
		record.Key = entry.Event.Session.ID[:]
	}
	result := k.producer.ProduceSync(ctx, record)
	if err := result.FirstErr(); err != nil {
		return 0, err
	}
	return result[0].Record.Offset, nil
}

func (k *Kafka) End(ctx context.Context, partition int32) (int64, error) {
	offsets, err := k.admin.ListEndOffsets(ctx, k.cfg.Topic)
	if err != nil {
		return 0, err
	}
	offset, ok := offsets.Lookup(k.cfg.Topic, partition)
	if !ok {
		return 0, fmt.Errorf("journal: no end offset for partition %d", partition)
	}
	return offset.Offset, offset.Err
}

func (k *Kafka) Read(ctx context.Context, partition int32, from, to int64, visit func(Entry) error) error {
	if from >= to {
		return nil
	}
	reader, err := kgo.NewClient(
		kgo.SeedBrokers(k.cfg.Brokers...),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{k.cfg.Topic: {partition: kgo.NewOffset().At(from)}}),
		kgo.FetchMaxWait(200*time.Millisecond),
	)
	if err != nil {
		return err
	}
	defer reader.Close()
	next := from
	for next < to {
		fetches := reader.PollFetches(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
		var failure error
		fetches.EachError(func(_ string, _ int32, err error) { failure = err })
		if failure != nil {
			return failure
		}
		var visitErr error
		fetches.EachRecord(func(record *kgo.Record) {
			if visitErr != nil || record.Offset < next || record.Offset >= to {
				return
			}
			entry, err := Decode(partition, record.Offset, record.Value)
			if err != nil {
				visitErr = err
				return
			}
			if err := visit(entry); err != nil {
				visitErr = err
				return
			}
			next = record.Offset + 1
		})
		if visitErr != nil {
			return visitErr
		}
	}
	return nil
}
