package journal

import (
	"errors"
	"fmt"
	"slices"

	"github.com/bvivg/axon/core/services/game/internal/session"
)

var (
	ErrGap      = errors.New("journal: event does not follow the known revision")
	ErrDiverged = errors.New("journal: replayed move diverged from the recorded outcome")
)

type Folded struct {
	Session session.Session
	Change  session.Change
	Applied bool
}

func Fold(rules session.Rules, base *session.Session, event Event) (Folded, error) {
	revision := event.Session.Revision
	if base != nil && revision <= base.Revision {
		return Folded{Session: *base, Change: event.Change}, nil
	}
	if !event.Partial {
		return Folded{Session: event.Session, Change: event.Change, Applied: true}, nil
	}
	if base == nil || revision != base.Revision+1 {
		known := uint32(0)
		if base != nil {
			known = base.Revision
		}
		return Folded{}, fmt.Errorf("%w: session %s at revision %d, event %d", ErrGap, event.Session.ID, known, revision)
	}
	next, change, err := rules.Redo(*base, event.Change)
	if err != nil {
		return Folded{}, fmt.Errorf("journal: redo move of %s: %w", event.Session.ID, err)
	}
	next.Revision, next.UpdatedAt = revision, change.At
	if event.Outcome != nil && !matches(*event.Outcome, next) {
		return Folded{}, fmt.Errorf("%w: session %s revision %d", ErrDiverged, event.Session.ID, revision)
	}
	return Folded{Session: next, Change: change, Applied: true}, nil
}

func matches(recorded Outcome, game session.Session) bool {
	got := OutcomeOf(game)
	return recorded.Status == got.Status && recorded.Turn == got.Turn && recorded.Winner == got.Winner &&
		slices.Equal(recorded.Remaining, got.Remaining)
}
