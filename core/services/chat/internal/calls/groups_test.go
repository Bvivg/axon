package calls

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
)

func groupFixture(t *testing.T) (Call, []uuid.UUID, []EndpointRef, time.Time) {
	t.Helper()
	now := time.Now().UTC()
	users := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	refs := []EndpointRef{{ID: uuid.New(), Epoch: 1}, {ID: uuid.New(), Epoch: 1}, {ID: uuid.New(), Epoch: 1}}
	c := NewGroup(uuid.New(), uuid.Nil, users[0], uuid.New(), "audio", users, now)
	groupApply(t, &c, users[0], Command{Action: "start_group", Endpoint: refs[0], SFUCapable: true}, now)
	return c, users, refs, now
}

func groupApply(t *testing.T, c *Call, user uuid.UUID, cmd Command, now time.Time) {
	t.Helper()
	if c.TransportKind() == "sfu" && (cmd.Action == "ready" || cmd.Action == "cleanup" || cmd.Action == "reconnecting") {
		if p := c.Participant(user); p != nil {
			cmd.MediaVersion = p.MediaVersion
		}
	}
	changed, err := Apply(c, user, cmd, now)
	if err != nil || !changed {
		t.Fatalf("%s: changed=%v err=%v", cmd.Action, changed, err)
	}
}

func TestGroupActivationAndPersonalInvitation(t *testing.T) {
	c, users, refs, now := groupFixture(t)
	groupApply(t, &c, users[1], Command{Action: "accept", Endpoint: refs[1], SFUCapable: true}, now)
	groupApply(t, &c, users[0], Command{Action: "ready", Endpoint: refs[0], Generation: 1, Ready: true}, now)
	if c.State != Connecting {
		t.Fatal("one ready endpoint activated room")
	}
	groupApply(t, &c, users[1], Command{Action: "ready", Endpoint: refs[1], Generation: 1, Ready: true}, now)
	if c.State != Active || c.ReadyCount() != 2 {
		t.Fatal("two ready participants did not activate")
	}
	view := ToProtoFor(c, users[2])
	if view.State != callv1.CallState_CALL_STATE_RINGING {
		t.Fatalf("invitation view=%s", view.State)
	}
	groupApply(t, &c, users[2], Command{Action: "accept", Endpoint: refs[2], SFUCapable: true}, now)
	if c.State != Active || c.Participant(users[2]).Ready {
		t.Fatal("third accept disrupted active media")
	}
	groupApply(t, &c, users[2], Command{Action: "ready", Endpoint: refs[2], Generation: 1, Ready: true}, now)
	groupApply(t, &c, users[0], Command{Action: "leave", Endpoint: refs[0]}, now)
	if c.State != Active || c.OwnerID != users[1] || c.ReadyCount() != 2 {
		t.Fatal("owner leave failed to preserve remaining room")
	}
}

func TestGroupReinviteFencesOldMediaVersion(t *testing.T) {
	c, users, refs, now := groupFixture(t)
	for i := 1; i < 3; i++ {
		groupApply(t, &c, users[i], Command{Action: "accept", Endpoint: refs[i], SFUCapable: true}, now)
	}
	for i := 0; i < 3; i++ {
		groupApply(t, &c, users[i], Command{Action: "ready", Endpoint: refs[i], Generation: 1, Ready: true}, now)
	}
	old := c.Participant(users[2]).MediaVersion
	groupApply(t, &c, users[0], Command{Action: "remove", Endpoint: refs[0], TargetID: users[2]}, now)
	if _, err := Apply(&c, users[0], Command{Action: "invite", Endpoint: refs[0], TargetID: users[2]}, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("reinvite before cleanup: %v", err)
	}
	groupApply(t, &c, users[2], Command{Action: "cleanup", Endpoint: refs[2], Generation: 1}, now)
	groupApply(t, &c, users[0], Command{Action: "invite", Endpoint: refs[0], TargetID: users[2]}, now)
	groupApply(t, &c, users[2], Command{Action: "accept", Endpoint: refs[2], SFUCapable: true}, now)
	if c.Participant(users[2]).MediaVersion <= old {
		t.Fatal("old token version was reused")
	}
	if _, err := Apply(&c, users[2], Command{Action: "ready", Endpoint: refs[2], Generation: 1, MediaVersion: old, Ready: true}, now); !errors.Is(err, ErrStale) {
		t.Fatalf("late ready from retired media accepted: %v", err)
	}
	groupApply(t, &c, users[0], Command{Action: "end", Endpoint: refs[0]}, now)
	if _, err := Apply(&c, users[2], Command{Action: "cleanup", Endpoint: refs[2], Generation: 1, MediaVersion: old}, now); !errors.Is(err, ErrStale) {
		t.Fatalf("late cleanup released new media: %v", err)
	}
}

func TestFallbackRequiresBothCleanupProofs(t *testing.T) {
	c, ea, eb, now := fixture(t)
	a, b := c.Participants[0].UserID, c.Participants[1].UserID
	c.Participant(a).SupportsSFU = true
	groupApply(t, &c, b, Command{Action: "accept", Endpoint: eb, SFUCapable: true, EnableSFU: true}, now)
	groupApply(t, &c, uuid.Nil, Command{Action: "timeout", EnableSFU: true}, c.Deadline)
	if c.Transport != "sfu" || c.TransportPhase != "waiting_cleanup" || c.Generation != 2 || c.CleanupGeneration != 1 {
		t.Fatal("missing fallback barrier")
	}
	at := c.Deadline.Add(-time.Millisecond)
	groupApply(t, &c, a, Command{Action: "cleanup", Endpoint: ea, Generation: 1}, at)
	if c.TransportPhase != "waiting_cleanup" {
		t.Fatal("one cleanup enabled SFU")
	}
	groupApply(t, &c, b, Command{Action: "cleanup", Endpoint: eb, Generation: 1}, at)
	if c.TransportPhase != "" || c.CleanupGeneration != 0 || c.State != Connecting {
		t.Fatal("both cleanup proofs did not enable SFU")
	}
	if _, err := Apply(&c, a, Command{Action: "ready", Endpoint: ea, Generation: 1, Ready: true}, at); !errors.Is(err, ErrStale) {
		t.Fatalf("old generation accepted: %v", err)
	}
}

func TestActiveP2PNeverFallsBack(t *testing.T) {
	c, ea, eb, now := fixture(t)
	a, b := c.Participants[0].UserID, c.Participants[1].UserID
	c.Participant(a).SupportsSFU = true
	groupApply(t, &c, b, Command{Action: "accept", Endpoint: eb, SFUCapable: true, EnableSFU: true}, now)
	for _, item := range []struct {
		u uuid.UUID
		e EndpointRef
	}{{a, ea}, {b, eb}} {
		groupApply(t, &c, item.u, Command{Action: "ready", Endpoint: item.e, Generation: 1, Ready: true}, now)
	}
	ea.Epoch++
	groupApply(t, &c, a, Command{Action: "resume", Endpoint: ea, SFUCapable: true, EnableSFU: true}, now)
	groupApply(t, &c, uuid.Nil, Command{Action: "timeout", EnableSFU: true}, c.Deadline)
	if c.TransportKind() != "p2p" || c.State != Ended {
		t.Fatal("active P2P migrated to SFU")
	}
}

func TestGroupInviteExpiryDoesNotShortenSetup(t *testing.T) {
	c, users, refs, now := groupFixture(t)
	late := now.Add(RingTimeout - time.Second)
	groupApply(t, &c, users[1], Command{Action: "accept", Endpoint: refs[1], SFUCapable: true}, late)
	setup := c.ConnectionDeadline
	groupApply(t, &c, uuid.Nil, Command{Action: "timeout"}, now.Add(RingTimeout))
	if c.State != Connecting || c.Participant(users[2]).Status != "missed" || !c.ConnectionDeadline.Equal(setup) {
		t.Fatal("unanswered invitation terminated connecting participants")
	}
	for i := 0; i < 2; i++ {
		groupApply(t, &c, users[i], Command{Action: "ready", Endpoint: refs[i], Generation: 1, Ready: true}, late.Add(2*time.Second))
	}
	if c.State != Active {
		t.Fatal("remaining participants could not connect")
	}
}

func TestGroupReconnectKeepsOtherParticipantsAndFencesOnlyResumedMember(t *testing.T) {
	c, users, refs, now := groupFixture(t)
	for i := 1; i < 3; i++ {
		groupApply(t, &c, users[i], Command{Action: "accept", Endpoint: refs[i], SFUCapable: true}, now)
	}
	for i := 0; i < 3; i++ {
		groupApply(t, &c, users[i], Command{Action: "ready", Endpoint: refs[i], Generation: 1, Ready: true}, now)
	}
	version := c.Participant(users[2]).MediaVersion
	groupApply(t, &c, users[2], Command{Action: "reconnecting", Endpoint: refs[2], Generation: 1}, now)
	if c.State != Active || c.ReadyCount() != 2 || c.Generation != 1 || c.Participant(users[2]).MediaVersion != version {
		t.Fatal("temporary reconnect disrupted other participants")
	}
	groupApply(t, &c, users[2], Command{Action: "ready", Endpoint: refs[2], Generation: 1, Ready: true}, now.Add(time.Second))
	refs[2].Epoch++
	groupApply(t, &c, users[2], Command{Action: "resume", Endpoint: refs[2], SFUCapable: true}, now.Add(2*time.Second))
	if c.State != Active || c.Generation != 1 || c.Participant(users[2]).MediaVersion <= version || !c.Participant(users[0]).Ready || !c.Participant(users[1]).Ready {
		t.Fatal("endpoint resume restarted the room")
	}
}
