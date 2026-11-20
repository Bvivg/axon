package cluster

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/projection"
)

const (
	OwnersGroup    = "game-owners"
	ProjectorGroup = "game-projector"
)

type Ownership struct {
	cfg   journal.KafkaConfig
	owner *owner.Owner
	log   *slog.Logger

	mu       sync.Mutex
	assigned map[int32]bool
}

func NewOwnership(cfg journal.KafkaConfig, own *owner.Owner, log *slog.Logger) *Ownership {
	return &Ownership{cfg: cfg, owner: own, log: log.With("component", "ownership"), assigned: map[int32]bool{}}
}

func (o *Ownership) Run(ctx context.Context) error {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(o.cfg.Brokers...),
		kgo.ConsumerGroup(OwnersGroup),
		kgo.ConsumeTopics(o.cfg.LeaseTopic),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.DisableAutoCommit(),
		kgo.SessionTimeout(10*time.Second),
		kgo.OnPartitionsAssigned(func(ctx context.Context, _ *kgo.Client, assigned map[string][]int32) {
			partitions := assigned[o.cfg.LeaseTopic]
			o.mu.Lock()
			for _, p := range partitions {
				o.assigned[p] = true
			}
			o.mu.Unlock()
			for _, p := range partitions {
				o.claim(ctx, p)
			}
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, revoked map[string][]int32) {
			o.release(revoked[o.cfg.LeaseTopic])
		}),
		kgo.OnPartitionsLost(func(_ context.Context, _ *kgo.Client, lost map[string][]int32) {
			o.release(lost[o.cfg.LeaseTopic])
		}),
	)
	if err != nil {
		return err
	}
	defer func() {
		o.owner.RevokeAll()
		leave, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.LeaveGroupContext(leave)
		client.Close()
	}()
	go o.reconcile(ctx)
	for ctx.Err() == nil {
		fetches := client.PollFetches(ctx)
		fetches.EachError(func(topic string, partition int32, err error) {
			if ctx.Err() == nil {
				o.log.WarnContext(ctx, "owner lease poll failed", "topic", topic, "partition", partition, "error", err)
			}
		})
	}
	return nil
}

func (o *Ownership) claim(ctx context.Context, partition int32) {
	if o.owner.Serving(partition) {
		return
	}
	started := time.Now()
	if err := o.owner.Assign(ctx, partition); err != nil {
		o.log.WarnContext(ctx, "partition claim failed", "partition", partition, "error", err)
		return
	}
	o.log.InfoContext(ctx, "partition claimed", "partition", partition, "duration_ms", time.Since(started).Milliseconds())
}

func (o *Ownership) release(partitions []int32) {
	o.mu.Lock()
	for _, p := range partitions {
		delete(o.assigned, p)
	}
	o.mu.Unlock()
	for _, p := range partitions {
		o.owner.Revoke(p)
		o.log.Info("partition released", "partition", p)
	}
}

func (o *Ownership) reconcile(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		o.mu.Lock()
		var missing []int32
		for p := range o.assigned {
			if !o.owner.Serving(p) {
				missing = append(missing, p)
			}
		}
		o.mu.Unlock()
		for _, p := range missing {
			o.claim(ctx, p)
		}
	}
}

type Projector struct {
	cfg   journal.KafkaConfig
	model *projection.Postgres
	log   *slog.Logger
}

func NewProjector(cfg journal.KafkaConfig, model *projection.Postgres, log *slog.Logger) *Projector {
	return &Projector{cfg: cfg, model: model, log: log.With("component", "projector")}
}

func (p *Projector) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := p.session(ctx); err != nil && ctx.Err() == nil {
			p.log.WarnContext(ctx, "projector restarting", "error", err)
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}
	return nil
}

func (p *Projector) session(ctx context.Context) error {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(p.cfg.Brokers...),
		kgo.ConsumerGroup(ProjectorGroup),
		kgo.ConsumeTopics(p.cfg.Topic),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.FetchMaxWait(100*time.Millisecond),
		kgo.OnPartitionsAssigned(func(context.Context, *kgo.Client, map[string][]int32) { p.model.Forget() }),
		kgo.OnPartitionsRevoked(func(context.Context, *kgo.Client, map[string][]int32) { p.model.Forget() }),
		kgo.OnPartitionsLost(func(context.Context, *kgo.Client, map[string][]int32) { p.model.Forget() }),
		kgo.AdjustFetchOffsetsFn(func(ctx context.Context, offsets map[string]map[int32]kgo.Offset) (map[string]map[int32]kgo.Offset, error) {
			for partition := range offsets[p.cfg.Topic] {
				projected, err := p.model.Projected(ctx, partition)
				if err != nil {
					return nil, err
				}
				offsets[p.cfg.Topic][partition] = kgo.NewOffset().At(projected.Offset)
			}
			return offsets, nil
		}),
	)
	if err != nil {
		return err
	}
	defer client.Close()
	for ctx.Err() == nil {
		fetches := client.PollRecords(ctx, 5000)
		var failure error
		fetches.EachError(func(_ string, _ int32, err error) {
			if ctx.Err() == nil {
				failure = err
			}
		})
		if failure != nil {
			client.AllowRebalance()
			return failure
		}
		batches := map[int32][]journal.Entry{}
		fetches.EachRecord(func(record *kgo.Record) {
			entry, err := journal.Decode(record.Partition, record.Offset, record.Value)
			if err != nil {
				p.log.ErrorContext(ctx, "skipping malformed journal record", "partition", record.Partition, "offset", record.Offset, "error", err)
				entry = journal.Entry{Partition: record.Partition, Offset: record.Offset}
			}
			batches[record.Partition] = append(batches[record.Partition], entry)
		})
		if len(batches) > 0 {
			if _, err := p.model.Apply(ctx, batches); err != nil {
				p.model.Forget()
				client.AllowRebalance()
				return err
			}
		}
		client.AllowRebalance()
	}
	return nil
}
