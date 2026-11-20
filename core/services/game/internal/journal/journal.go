package journal

import (
	"context"
	"errors"
	"hash/fnv"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/session"
)

var ErrOutOfRange = errors.New("journal: offset out of range")

type Claim struct {
	Generation int64
	Owner      string
	At         time.Time
}

type Event struct {
	Session    session.Session
	Change     session.Change
	Generation int64
}

type Entry struct {
	Partition int32
	Offset    int64
	Event     *Event
	Claim     *Claim
}

type Log interface {
	Partitions() int32
	Append(ctx context.Context, partition int32, entry Entry) (int64, error)
	End(ctx context.Context, partition int32) (int64, error)
	Read(ctx context.Context, partition int32, from, to int64, visit func(Entry) error) error
}

func PartitionOf(id uuid.UUID, partitions int32) int32 {
	h := fnv.New32a()
	_, _ = h.Write(id[:])
	return int32(h.Sum32() % uint32(partitions))
}

type Fence struct {
	claimed map[int32]int64
}

func NewFence() *Fence { return &Fence{claimed: map[int32]int64{}} }

func (f *Fence) Seed(partition int32, generation int64) {
	if generation > f.claimed[partition] {
		f.claimed[partition] = generation
	}
}

func (f *Fence) Generation(partition int32) int64 { return f.claimed[partition] }

func (f *Fence) Admit(entry Entry) bool {
	if entry.Claim != nil {
		f.Seed(entry.Partition, entry.Claim.Generation)
		return false
	}
	return entry.Event != nil && entry.Event.Generation >= f.claimed[entry.Partition]
}
