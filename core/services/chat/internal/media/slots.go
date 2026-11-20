package media

import (
	"context"
	"sync"
)

type ownerKey struct{}

func WithOwner(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, ownerKey{}, owner)
}

func OwnerOf(ctx context.Context) string {
	owner, _ := ctx.Value(ownerKey{}).(string)
	return owner
}

type slotWaiter struct {
	ready   chan struct{}
	granted bool
}

type Slots struct {
	mu       sync.Mutex
	free     int
	perOwner int
	held     map[string]int
	waiting  map[string][]*slotWaiter
	turns    []string
}

func NewSlots(capacity, perOwner int) *Slots {
	capacity = max(capacity, 1)
	if perOwner <= 0 || perOwner > capacity {
		perOwner = capacity
	}
	return &Slots{
		free:     capacity,
		perOwner: perOwner,
		held:     map[string]int{},
		waiting:  map[string][]*slotWaiter{},
	}
}

func (s *Slots) Acquire(ctx context.Context, owner string) (func(), error) {
	w := &slotWaiter{ready: make(chan struct{})}

	s.mu.Lock()
	if len(s.waiting[owner]) == 0 {
		s.turns = append(s.turns, owner)
	}
	s.waiting[owner] = append(s.waiting[owner], w)
	s.dispatch()
	s.mu.Unlock()

	select {
	case <-w.ready:
		return s.releaser(owner), nil
	case <-ctx.Done():
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if w.granted {
		s.release(owner)
	} else {
		s.forget(owner, w)
	}
	return nil, ctx.Err()
}

func (s *Slots) releaser(owner string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.release(owner)
		})
	}
}

func (s *Slots) release(owner string) {
	s.free++
	if s.held[owner]--; s.held[owner] <= 0 {
		delete(s.held, owner)
	}
	s.dispatch()
}

func (s *Slots) forget(owner string, w *slotWaiter) {
	queue := s.waiting[owner]
	for i, queued := range queue {
		if queued == w {
			queue = append(queue[:i:i], queue[i+1:]...)
			break
		}
	}
	if len(queue) > 0 {
		s.waiting[owner] = queue
		return
	}
	delete(s.waiting, owner)
	for i, turn := range s.turns {
		if turn == owner {
			s.turns = append(s.turns[:i:i], s.turns[i+1:]...)
			break
		}
	}
}

func (s *Slots) dispatch() {
	for s.free > 0 {
		if !s.grantNext() {
			return
		}
	}
}

func (s *Slots) grantNext() bool {
	for range len(s.turns) {
		owner := s.turns[0]
		s.turns = s.turns[1:]
		if s.held[owner] >= s.perOwner {
			s.turns = append(s.turns, owner)
			continue
		}

		queue := s.waiting[owner]
		w := queue[0]
		if len(queue) > 1 {
			s.waiting[owner] = queue[1:]
			s.turns = append(s.turns, owner)
		} else {
			delete(s.waiting, owner)
		}

		s.free--
		s.held[owner]++
		w.granted = true
		close(w.ready)
		return true
	}
	return false
}
