package calls

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func fixture(t *testing.T) (Call, EndpointRef, EndpointRef, time.Time) {
	t.Helper()
	now := time.Now().UTC()
	a, b := uuid.New(), uuid.New()
	ea := EndpointRef{ID: uuid.New(), Epoch: 1}
	eb := EndpointRef{ID: uuid.New(), Epoch: 1}
	c := New(uuid.New(), uuid.New(), a, b, uuid.New(), "audio", now)
	if _, err := Apply(&c, a, Command{Action: "start", Endpoint: ea}, now); err != nil {
		t.Fatal(err)
	}
	return c, ea, eb, now
}
func TestActivationRequiresBothSelectedEndpoints(t *testing.T) {
	c, ea, eb, now := fixture(t)
	a, b := c.Participants[0].UserID, c.Participants[1].UserID
	if _, err := Apply(&c, b, Command{Action: "accept", Endpoint: eb}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(&c, a, Command{Action: "ready", Endpoint: EndpointRef{ID: uuid.New(), Epoch: 1}, Generation: 1, Ready: true}, now); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unselected endpoint: %v", err)
	}
	for i, item := range []struct {
		u uuid.UUID
		e EndpointRef
	}{{a, ea}, {b, eb}} {
		if _, err := Apply(&c, item.u, Command{Action: "ready", Endpoint: item.e, Generation: 1, Ready: true}, now); err != nil {
			t.Fatal(err)
		}
		if i == 0 && c.State != Connecting {
			t.Fatal("activated with one endpoint")
		}
	}
	if c.State != Active || !c.EverActive || c.StartedAt == nil {
		t.Fatal("not active")
	}
	if _, err := Apply(&c, a, Command{Action: "end", Endpoint: ea}, now); err != nil {
		t.Fatal(err)
	}
	if c.Clean() {
		t.Fatal("cleanup assumed before client proof")
	}
	for _, item := range []struct {
		u uuid.UUID
		e EndpointRef
	}{{a, ea}, {b, eb}} {
		if _, err := Apply(&c, item.u, Command{Action: "cleanup", Endpoint: item.e, Generation: 1}, now); err != nil {
			t.Fatal(err)
		}
	}
	if !c.Clean() || !c.Deadline.IsZero() {
		t.Fatal("cleanup not terminal")
	}
}
func TestResumeFencesOldGenerationAndKeepsOriginalStart(t *testing.T) {
	c, ea, eb, now := fixture(t)
	a, b := c.Participants[0].UserID, c.Participants[1].UserID
	_, _ = Apply(&c, b, Command{Action: "accept", Endpoint: eb}, now)
	for _, item := range []struct {
		u uuid.UUID
		e EndpointRef
	}{{a, ea}, {b, eb}} {
		_, _ = Apply(&c, item.u, Command{Action: "ready", Endpoint: item.e, Generation: 1, Ready: true}, now)
	}
	started := *c.StartedAt
	ea.Epoch++
	if _, err := Apply(&c, a, Command{Action: "resume", Endpoint: ea}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if c.State != Connecting || c.Generation != 2 || !c.EverActive || !c.StartedAt.Equal(started) {
		t.Fatal("resume lost fences or original start")
	}
	if _, err := Apply(&c, b, Command{Action: "ready", Endpoint: eb, Generation: 1, Ready: true}, now); !errors.Is(err, ErrStale) {
		t.Fatalf("old generation: %v", err)
	}
}
func TestLeaseReleaseDoesNotClaimHardwareCleanup(t *testing.T) {
	c, ea, _, now := fixture(t)
	a := c.CallerID
	_, _ = Apply(&c, a, Command{Action: "end", Endpoint: ea}, now)
	_, err := Apply(&c, uuid.Nil, Command{Action: "timeout"}, now.Add(Lease))
	if err != nil {
		t.Fatal(err)
	}
	p := c.Participant(a)
	if !p.CleanupExpired || p.CleanupDone || !c.Clean() {
		t.Fatal("lease expiry must remain distinct from cleanup proof")
	}
}

func TestResumeAfterEndCanAcknowledgeCleanupWithoutReviving(t *testing.T) {
	c, ea, _, now := fixture(t)
	a := c.CallerID
	_, _ = Apply(&c, a, Command{Action: "end", Endpoint: ea}, now)
	generation := c.Generation
	ea.Epoch++
	if _, err := Apply(&c, a, Command{Action: "resume", Endpoint: ea}, now); err != nil {
		t.Fatal(err)
	}
	if c.State != Ended || c.Generation != generation || c.Participant(a).Epoch != ea.Epoch {
		t.Fatal("terminal resume lost fences")
	}
	if _, err := Apply(&c, a, Command{Action: "cleanup", Endpoint: ea, Generation: generation}, now); err != nil {
		t.Fatal(err)
	}
	if !c.Clean() {
		t.Fatal("resumed endpoint could not release slot")
	}
}
