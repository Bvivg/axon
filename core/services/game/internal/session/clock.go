package session

import (
	"slices"
	"time"
)

const TimeLimitMS int64 = 300_000

type Clock struct {
	RemainingMS []int64   `json:"remaining_ms"`
	StartedAt   time.Time `json:"started_at"`
}

func NewClock(now time.Time, limit int64, seats int) *Clock {
	remaining := make([]int64, max(seats, 2))
	for seat := range remaining {
		remaining[seat] = limit
	}
	return &Clock{RemainingMS: remaining, StartedAt: now}
}

func (s Session) ClockAt(now time.Time) *Clock {
	if s.Clock == nil {
		return nil
	}
	clock := *s.Clock
	clock.RemainingMS = slices.Clone(clock.RemainingMS)
	if !s.State.Over() && s.State.Turn >= 0 && s.State.Turn < len(clock.RemainingMS) {
		elapsed := max(int64(0), now.Sub(clock.StartedAt).Milliseconds())
		clock.RemainingMS[s.State.Turn] = max(int64(0), clock.RemainingMS[s.State.Turn]-elapsed)
	}
	clock.StartedAt = now
	return &clock
}

func (s *Session) expire(now time.Time) bool {
	if s.Clock == nil || s.State.Over() || s.State.Turn < 0 || s.State.Turn >= len(s.Clock.RemainingMS) || s.Automated(s.State.Turn) {
		return false
	}
	clock := s.ClockAt(now)
	if clock.RemainingMS[s.State.Turn] > 0 {
		return false
	}
	s.Clock = clock
	if s.Multiseat() {
		s.Left = slices.Concat(s.Left, []int{s.State.Turn})
		return true
	}
	s.State = s.State.Finish(1 - s.State.Turn)
	s.TimedOut = true
	return true
}
