package journal

import (
	"context"
	"sync"
)

type Memory struct {
	mu         sync.Mutex
	partitions [][][]byte
	fail       func(partition int32) error
}

func NewMemory(partitions int32) *Memory {
	return &Memory{partitions: make([][][]byte, partitions)}
}

func (m *Memory) FailWith(fail func(partition int32) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = fail
}

func (m *Memory) Partitions() int32 { return int32(len(m.partitions)) }

func (m *Memory) Append(_ context.Context, partition int32, entry Entry) (int64, error) {
	raw, err := Encode(entry)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		if err := m.fail(partition); err != nil {
			return 0, err
		}
	}
	m.partitions[partition] = append(m.partitions[partition], raw)
	return int64(len(m.partitions[partition]) - 1), nil
}

func (m *Memory) End(_ context.Context, partition int32) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.partitions[partition])), nil
}

func (m *Memory) Read(ctx context.Context, partition int32, from, to int64, visit func(Entry) error) error {
	m.mu.Lock()
	records := m.partitions[partition]
	if from < 0 || to > int64(len(records)) || from > to {
		m.mu.Unlock()
		return ErrOutOfRange
	}
	window := append([][]byte(nil), records[from:to]...)
	m.mu.Unlock()
	for i, raw := range window {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := Decode(partition, from+int64(i), raw)
		if err != nil {
			return err
		}
		if err := visit(entry); err != nil {
			return err
		}
	}
	return nil
}
