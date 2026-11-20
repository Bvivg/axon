package rebuild

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

var (
	ErrTruncated = errors.New("rebuild: the topic no longer starts at offset zero")
	ErrStalled   = errors.New("rebuild: the topic stopped short of its end offsets")
)

type Kafka struct {
	Brokers []string
	Topic   string
	Idle    time.Duration
}

func (k Kafka) bounds(ctx context.Context) (map[int32]int64, error) {
	client, err := kgo.NewClient(kgo.SeedBrokers(k.Brokers...))
	if err != nil {
		return nil, err
	}
	defer client.Close()
	admin := kadm.NewClient(client)
	starts, err := admin.ListStartOffsets(ctx, k.Topic)
	if err != nil {
		return nil, err
	}
	ends, err := admin.ListEndOffsets(ctx, k.Topic)
	if err != nil {
		return nil, err
	}
	if err := ends.Error(); errors.Is(err, kerr.UnknownTopicOrPartition) {
		return map[int32]int64{}, nil
	} else if err != nil {
		return nil, err
	}
	bounds := map[int32]int64{}
	var failure error
	ends.Each(func(end kadm.ListedOffset) {
		start, ok := starts.Lookup(k.Topic, end.Partition)
		if !ok || start.Err != nil {
			failure = fmt.Errorf("rebuild: no start offset for partition %d", end.Partition)
			return
		}
		if start.Offset > 0 {
			failure = fmt.Errorf("%w: partition %d starts at %d", ErrTruncated, end.Partition, start.Offset)
			return
		}
		if end.Offset > 0 {
			bounds[end.Partition] = end.Offset
		}
	})
	return bounds, failure
}

func (k Kafka) Read(ctx context.Context, visit func(kind string, value []byte) error) error {
	bounds, err := k.bounds(ctx)
	if err != nil || len(bounds) == 0 {
		return err
	}
	offsets := map[int32]kgo.Offset{}
	for partition := range bounds {
		offsets[partition] = kgo.NewOffset().At(0)
	}
	reader, err := kgo.NewClient(
		kgo.SeedBrokers(k.Brokers...),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{k.Topic: offsets}),
		kgo.FetchMaxWait(200*time.Millisecond),
	)
	if err != nil {
		return err
	}
	defer reader.Close()
	idle := k.Idle
	if idle <= 0 {
		idle = 15 * time.Second
	}
	next := map[int32]int64{}
	progress := time.Now()
	for remaining(next, bounds) {
		poll, cancel := context.WithTimeout(ctx, idle)
		fetches := reader.PollFetches(poll)
		cancel()
		if err := ctx.Err(); err != nil {
			return err
		}
		var failure error
		fetches.EachError(func(_ string, _ int32, err error) {
			if !errors.Is(err, context.DeadlineExceeded) {
				failure = err
			}
		})
		if failure != nil {
			return failure
		}
		fetches.EachRecord(func(record *kgo.Record) {
			if failure != nil || record.Offset < next[record.Partition] || record.Offset >= bounds[record.Partition] {
				return
			}
			if failure = visit(header(record, outbox.TypeHeader), record.Value); failure == nil {
				next[record.Partition] = record.Offset + 1
				progress = time.Now()
			}
		})
		if failure != nil {
			return failure
		}
		if time.Since(progress) > idle {
			return fmt.Errorf("%w: read %v of %v", ErrStalled, next, bounds)
		}
	}
	return nil
}

func remaining(next, bounds map[int32]int64) bool {
	for partition, end := range bounds {
		if next[partition] < end {
			return true
		}
	}
	return false
}

func header(record *kgo.Record, key string) string {
	for _, h := range record.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}
