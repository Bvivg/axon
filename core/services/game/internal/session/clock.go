package session

import "time"

const TimeLimitMS int64 = 300_000

type Clock struct {
	RemainingMS [2]int64  `json:"remaining_ms"`
	StartedAt   time.Time `json:"started_at"`
}

func NewClock(now time.Time, limit int64) *Clock {
	return &Clock{RemainingMS: [2]int64{limit, limit}, StartedAt: now}
}

func (s Session) ClockAt(now time.Time) *Clock {
	if s.Clock == nil {
		return nil
	}
	clock := *s.Clock
	if !s.State.Over() && s.State.Turn >= 0 && s.State.Turn < len(clock.RemainingMS) {
		elapsed := max(int64(0), now.Sub(clock.StartedAt).Milliseconds())
		clock.RemainingMS[s.State.Turn] = max(int64(0), clock.RemainingMS[s.State.Turn]-elapsed)
	}
	clock.StartedAt = now
	return &clock
}

func (s *Session) expire(now time.Time) bool {
	if s.Clock == nil || s.State.Over() {
		return false
	}
	clock := s.ClockAt(now)
	if clock.RemainingMS[s.State.Turn] > 0 {
		return false
	}
	s.Clock = clock
	s.State = s.State.Finish(1 - s.State.Turn)
	s.TimedOut = true
	return true
}
