package journal

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/bvivg/axon/core/shared/pkg/telemetry"
)

type KafkaConfig struct {
	Brokers           []string
	Topic             string
	LeaseTopic        string
	Partitions        int32
	ReplicationFactor int16
	MinInSync         int
	Linger            time.Duration
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
		kgo.ProducerLinger(cfg.Linger),
		kgo.ProducerBatchCompression(kgo.Lz4Compression(), kgo.NoCompression()),
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

var retainForever = map[string]string{"retention.ms": "-1", "cleanup.policy": "delete"}

func (k *Kafka) ensureTopics(ctx context.Context) error {
	shared := map[string]*string{}
	if k.cfg.MinInSync > 0 {
		value := fmt.Sprint(k.cfg.MinInSync)
		shared["min.insync.replicas"] = &value
	}
	journal := maps.Clone(shared)
	for key, value := range retainForever {
		journal[key] = &value
	}
	existed, err := k.createTopic(ctx, k.cfg.Topic, journal)
	if err != nil {
		return err
	}
	if existed {
		if err := k.keepForever(ctx); err != nil {
			return err
		}
	}
	if _, err := k.createTopic(ctx, k.cfg.LeaseTopic, shared); err != nil {
		return err
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

func (k *Kafka) createTopic(ctx context.Context, topic string, configs map[string]*string) (bool, error) {
	_, err := k.admin.CreateTopic(ctx, k.cfg.Partitions, k.cfg.ReplicationFactor, configs, topic)
	switch {
	case errors.Is(err, kerr.TopicAlreadyExists):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("journal: create topic %s: %w", topic, err)
	}
	return false, nil
}

func (k *Kafka) keepForever(ctx context.Context) error {
	described, err := k.admin.DescribeTopicConfigs(ctx, k.cfg.Topic)
	if err != nil {
		return fmt.Errorf("journal: describe topic %s: %w", k.cfg.Topic, err)
	}
	current, err := described.On(k.cfg.Topic, nil)
	if err == nil {
		err = current.Err
	}
	if err != nil {
		return fmt.Errorf("journal: describe topic %s: %w", k.cfg.Topic, err)
	}
	var changes []kadm.AlterConfig
	for key, want := range retainForever {
		if !slices.ContainsFunc(current.Configs, func(c kadm.Config) bool { return c.Key == key && c.Value != nil && *c.Value == want }) {
			changes = append(changes, kadm.AlterConfig{Op: kadm.SetConfig, Name: key, Value: &want})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	altered, err := k.admin.AlterTopicConfigs(ctx, changes, k.cfg.Topic)
	if err == nil {
		var result kadm.AlterConfigsResponse
		if result, err = altered.On(k.cfg.Topic, nil); err == nil {
			err = result.Err
		}
	}
	if err != nil {
		return fmt.Errorf("journal: keep topic %s forever: %w", k.cfg.Topic, err)
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
	ctx, span := tracer.Start(ctx, "journal.append", trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(attribute.String("messaging.destination.name", k.cfg.Topic), attribute.Int("messaging.destination.partition.id", int(partition))))
	defer span.End()
	record := &kgo.Record{Topic: k.cfg.Topic, Partition: partition, Value: raw}
	if entry.Event != nil {
		record.Key = entry.Event.Session.ID[:]
	}
	telemetry.Inject(ctx, Headers{record: record})
	result := k.producer.ProduceSync(ctx, record)
	if err := result.FirstErr(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "append failed")
		return 0, err
	}
	span.SetAttributes(attribute.Int64("messaging.kafka.offset", result[0].Record.Offset))
	return result[0].Record.Offset, nil
}

var tracer = telemetry.Tracer("axon/game/journal")

type Headers struct{ record *kgo.Record }

func (h Headers) Get(key string) string {
	for _, header := range h.record.Headers {
		if header.Key == key {
			return string(header.Value)
		}
	}
	return ""
}

func (h Headers) Set(key, value string) {
	for i, header := range h.record.Headers {
		if header.Key == key {
			h.record.Headers[i].Value = []byte(value)
			return
		}
	}
	h.record.Headers = append(h.record.Headers, kgo.RecordHeader{Key: key, Value: []byte(value)})
}

func (h Headers) Keys() []string {
	keys := make([]string, 0, len(h.record.Headers))
	for _, header := range h.record.Headers {
		keys = append(keys, header.Key)
	}
	return keys
}

func TraceOf(record *kgo.Record) trace.SpanContext {
	return trace.SpanContextFromContext(telemetry.Extract(context.Background(), Headers{record: record}))
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
