package calls

import (
	"time"

	"github.com/google/uuid"
)

func BeginFallback(c *Call, now time.Time) {
	c.CleanupGeneration = c.Generation
	c.Generation++
	c.NegotiationID = uuid.New()
	c.Transport = "sfu"
	c.TransportPhase = "waiting_cleanup"
	c.Deadline = now.Add(CleanupTimeout)
	for i := range c.Participants {
		c.Participants[i].Ready = false
		c.Participants[i].CleanupDone = false
		c.Participants[i].CleanupExpired = false
	}
}
