package calls

import (
	"time"

	"github.com/google/uuid"
)

func (c Call) InvitationDeadline(user uuid.UUID, now time.Time) (time.Time, bool) {
	p := c.Participant(user)
	if c.State == Ended || c.TransportPhase == "waiting_cleanup" || p == nil || p.Status != "invited" {
		return time.Time{}, false
	}
	deadline := c.Deadline
	if c.Group() {
		deadline = p.InviteUntil
	}
	if !c.Group() && (c.State != Ringing || user == c.CallerID) {
		return time.Time{}, false
	}
	return deadline, now.Before(deadline)
}
