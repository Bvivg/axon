package service_test

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/service"
)

type recordingPublisher struct {
	mu       sync.Mutex
	families []uuid.UUID
	users    []uuid.UUID
}

func (p *recordingPublisher) PublishFamily(_ context.Context, familyID uuid.UUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.families = append(p.families, familyID)
	return nil
}

func (p *recordingPublisher) PublishUser(_ context.Context, userID uuid.UUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.users = append(p.users, userID)
	return nil
}

func (p *recordingPublisher) publishedFamilies() map[uuid.UUID]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[uuid.UUID]bool, len(p.families))
	for _, id := range p.families {
		out[id] = true
	}
	return out
}

func withRevocation(p *recordingPublisher) harnessOption {
	return func(cfg *service.Config) { cfg.Revocation = p }
}
