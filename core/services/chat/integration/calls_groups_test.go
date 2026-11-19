//go:build integration

package integration

import (
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
)

func sfuRepo(t *testing.T) *repository.Repository {
	t.Helper()
	newRepo(t)
	return repository.NewWithOptions(pool, repository.Options{SFU: true})
}
func sfuPeer(t *testing.T, r *repository.Repository) callPeer {
	t.Helper()
	p := callUser(t, r)
	if err := r.SetEndpointSFU(t.Context(), p.actor, p.ref, true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return p
}
func sfuCommand(t *testing.T, r *repository.Repository, p callPeer, cmd calls.Command) calls.Call {
	t.Helper()
	cmd.SFUCapable = true
	cmd.EnableSFU = true
	if cmd.Action == "ready" || cmd.Action == "cleanup" || cmd.Action == "reconnecting" {
		current, err := r.GetCall(t.Context(), p.actor.UserID, cmd.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.TransportKind() == "sfu" {
			cmd.MediaVersion = current.Participant(p.actor.UserID).MediaVersion
		}
	}
	if cmd.Action == "start_group" && cmd.ID == uuid.Nil {
		cmd.ID = uuid.New()
	}
	c, err := callCommand(r, p, cmd)
	if err != nil {
		t.Fatalf("%s: %v", cmd.Action, err)
	}
	return c
}
func groupRoom(t *testing.T, peers ...callPeer) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(t.Context(), `INSERT INTO rooms(id,name,created_by,kind) VALUES($1,'Group',$2,'group')`, id, peers[0].actor.UserID); err != nil {
		t.Fatal(err)
	}
	for _, p := range peers {
		if _, err := pool.Exec(t.Context(), `INSERT INTO room_members(room_id,user_id) VALUES($1,$2)`, id, p.actor.UserID); err != nil {
			t.Fatal(err)
		}
	}
	return id
}
func TestCallGroupInvitationsPushAndMediaFence(t *testing.T) {
	r := sfuRepo(t)
	a, b, c := sfuPeer(t, r), sfuPeer(t, r), sfuPeer(t, r)
	room := groupRoom(t, a, b, c)
	call := sfuCommand(t, r, a, calls.Command{Action: "start_group", RoomID: room, Mode: "audio", Users: []uuid.UUID{b.actor.UserID}})
	jobs, err := r.DueCallPush(t.Context(), time.Now().UTC())
	if err != nil || len(jobs) != 1 || jobs[0].Event.UserID != b.actor.UserID {
		t.Fatalf("initial invitation: %v %v", jobs, err)
	}
	if err = r.RecordCallPush(t.Context(), jobs[0].Event.ID, "", true); err != nil {
		t.Fatal(err)
	}
	sfuCommand(t, r, b, calls.Command{Action: "accept", ID: call.ID})
	for _, p := range []callPeer{a, b} {
		call = sfuCommand(t, r, p, calls.Command{Action: "ready", ID: call.ID, Generation: 1, Ready: true})
	}
	call = sfuCommand(t, r, a, calls.Command{Action: "invite", ID: call.ID, TargetID: c.actor.UserID})
	if call.State != calls.Active {
		t.Fatal("invite interrupted active call")
	}
	jobs, err = r.DueCallPush(t.Context(), time.Now().UTC())
	if err != nil || len(jobs) != 1 || jobs[0].Event.UserID != c.actor.UserID || jobs[0].Event.Call.Version <= 1 {
		t.Fatalf("active invitation missing: %v %v", jobs, err)
	}
	if err = r.RecordCallPush(t.Context(), jobs[0].Event.ID, "", true); err != nil {
		t.Fatal(err)
	}
	call = sfuCommand(t, r, c, calls.Command{Action: "accept", ID: call.ID})
	old := call.Participant(c.actor.UserID).MediaVersion
	sfuCommand(t, r, c, calls.Command{Action: "ready", ID: call.ID, Generation: 1, Ready: true})
	sfuCommand(t, r, a, calls.Command{Action: "remove", ID: call.ID, TargetID: c.actor.UserID})
	sfuCommand(t, r, c, calls.Command{Action: "cleanup", ID: call.ID, Generation: 1})
	sfuCommand(t, r, a, calls.Command{Action: "invite", ID: call.ID, TargetID: c.actor.UserID})
	call = sfuCommand(t, r, c, calls.Command{Action: "accept", ID: call.ID})
	if call.Participant(c.actor.UserID).MediaVersion <= old {
		t.Fatal("token fence reused")
	}
	var slots int
	if err = pool.QueryRow(t.Context(), `SELECT count(*) FROM call_user_slots WHERE call_id=$1`, call.ID).Scan(&slots); err != nil || slots != 3 {
		t.Fatalf("slots=%d err=%v", slots, err)
	}
}

func TestCallReplacementAtomicACLAndCleanup(t *testing.T) {
	t.Run("chat", func(t *testing.T) { testCallReplacement(t, false) })
	t.Run("roomless", func(t *testing.T) { testCallReplacement(t, true) })
}

func testCallReplacement(t *testing.T, roomless bool) {
	t.Helper()
	r := sfuRepo(t)
	a, b, c := sfuPeer(t, r), sfuPeer(t, r), sfuPeer(t, r)
	start := calls.Command{Action: "start_direct", TargetID: b.actor.UserID, Mode: "audio"}
	wantRooms := 1
	if !roomless {
		start = calls.Command{Action: "start", RoomID: directCallRoom(t, a, b), Mode: "audio"}
		wantRooms++
	}
	old := sfuCommand(t, r, a, start)
	sfuCommand(t, r, b, calls.Command{Action: "accept", ID: old.ID})
	for _, p := range []callPeer{a, b} {
		old = sfuCommand(t, r, p, calls.Command{Action: "ready", ID: old.ID, Generation: 1, Ready: true})
	}
	cmd := calls.Command{Action: "replace", ID: old.ID, TargetID: c.actor.UserID, Endpoint: a.ref, RequestID: uuid.New()}
	if _, err := r.ReplaceCall(t.Context(), a.actor, cmd, time.Now().UTC()); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("replacement invited a stranger: %v", err)
	}
	directCallRoom(t, a, c)
	group, err := r.ReplaceCall(t.Context(), a.actor, cmd, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if group.ID == old.ID || group.RoomID != uuid.Nil || group.PredecessorID != old.ID || group.TransportPhase != "waiting_cleanup" {
		t.Fatal("incorrect replacement identity")
	}
	again, err := r.ReplaceCall(t.Context(), a.actor, cmd, time.Now().UTC())
	if err != nil || again.ID != group.ID {
		t.Fatalf("replay %v", err)
	}
	if _, err = r.GetCall(t.Context(), c.actor.UserID, old.ID); !errors.Is(err, calls.ErrNotFound) {
		t.Fatalf("third gained direct history: %v", err)
	}
	if calls.ToProtoFor(group, c.actor.UserID).PredecessorCallId != nil {
		t.Fatal("third sees source call ID")
	}
	var slots, rooms int
	if err = pool.QueryRow(t.Context(), `SELECT count(*) FROM call_user_slots WHERE call_id=$1`, group.ID).Scan(&slots); err != nil || slots != 3 {
		t.Fatalf("slot transfer %d %v", slots, err)
	}
	if err = pool.QueryRow(t.Context(), `SELECT count(*) FROM rooms`).Scan(&rooms); err != nil || rooms != wantRooms {
		t.Fatalf("unexpected new chat %d %v", rooms, err)
	}
	sfuCommand(t, r, a, calls.Command{Action: "cleanup", ID: old.ID, Generation: 1})
	current, err := r.GetCall(t.Context(), a.actor.UserID, group.ID)
	if err != nil || current.TransportPhase != "waiting_cleanup" {
		t.Fatal("one ACK bypassed barrier", err)
	}
	sfuCommand(t, r, b, calls.Command{Action: "cleanup", ID: old.ID, Generation: 1})
	current, err = r.GetCall(t.Context(), c.actor.UserID, group.ID)
	if err != nil || current.TransportPhase != "" || current.State != calls.Ringing {
		t.Fatal("barrier not released", err)
	}
	jobs, err := r.DueCallPush(t.Context(), time.Now().UTC())
	if err != nil || len(jobs) != 1 || jobs[0].Event.Call.ID != group.ID {
		t.Fatalf("replacement push missing %v %v", jobs, err)
	}
}

func TestCallWebhookReceiptsCannotReuseEventID(t *testing.T) {
	r := sfuRepo(t)
	hash := sha256.Sum256([]byte("signed body"))
	id := uuid.NewString()
	for i := 0; i < 2; i++ {
		if err := r.RecordWebhook(t.Context(), id, hash[:], "call_"+uuid.NewString(), "participant_joined"); err != nil {
			t.Fatal(err)
		}
	}
	other := sha256.Sum256([]byte("different body"))
	if err := r.RecordWebhook(t.Context(), id, other[:], "call_"+uuid.NewString(), "room_finished"); !errors.Is(err, calls.ErrConflict) {
		t.Fatalf("event collision accepted: %v", err)
	}
}

func TestCallSFUMigrationRoundTripAndR1SchemaCompatibility(t *testing.T) {
	r := sfuRepo(t)
	if _, err := pool.Exec(t.Context(), `TRUNCATE call_webhook_receipts`); err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../migrations/000015_calls_sfu.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../migrations/000015_calls_sfu.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(t.Context(), string(down)); err != nil {
		t.Fatal(err)
	}
	legacy := repository.New(pool)
	a, b := callUser(t, legacy), callUser(t, legacy)
	c, err := callCommand(legacy, a, calls.Command{Action: "start", RoomID: directCallRoom(t, a, b), Mode: "audio"})
	if err != nil {
		t.Fatal("schema14 start", err)
	}
	c, err = callCommand(legacy, b, calls.Command{Action: "accept", ID: c.ID})
	if err != nil {
		t.Fatal("schema14 accept", err)
	}
	if _, err = pool.Exec(t.Context(), string(up)); err != nil {
		t.Fatal(err)
	}
	if _, err = r.GetCall(t.Context(), a.actor.UserID, c.ID); err != nil {
		t.Fatal("history after additive upgrade", err)
	}
	hash := sha256.Sum256([]byte("preserve receipt"))
	if err = r.RecordWebhook(t.Context(), uuid.NewString(), hash[:], "call_"+c.ID.String(), "room_finished"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(t.Context(), string(down)); err == nil {
		t.Fatal("down destroyed nonempty receipt history")
	}
	var receipts int
	if err = pool.QueryRow(t.Context(), `SELECT count(*) FROM call_webhook_receipts`).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("receipt history lost %d %v", receipts, err)
	}
}

func TestCallReplacementBusyThirdLeavesSourceActive(t *testing.T) {
	r := sfuRepo(t)
	a, b, c, d := sfuPeer(t, r), sfuPeer(t, r), sfuPeer(t, r), sfuPeer(t, r)
	room := directCallRoom(t, a, b)
	directCallRoom(t, a, c)
	source := sfuCommand(t, r, a, calls.Command{Action: "start", RoomID: room, Mode: "audio"})
	sfuCommand(t, r, b, calls.Command{Action: "accept", ID: source.ID})
	for _, p := range []callPeer{a, b} {
		source = sfuCommand(t, r, p, calls.Command{Action: "ready", ID: source.ID, Generation: 1, Ready: true})
	}
	sfuCommand(t, r, c, calls.Command{Action: "start", RoomID: directCallRoom(t, c, d), Mode: "audio"})
	_, err := r.ReplaceCall(t.Context(), a.actor, calls.Command{Action: "replace", ID: source.ID, TargetID: c.actor.UserID, Endpoint: a.ref, RequestID: uuid.New()}, time.Now().UTC())
	if !errors.Is(err, calls.ErrBusy) {
		t.Fatalf("busy replacement=%v", err)
	}
	current, err := r.GetCall(t.Context(), a.actor.UserID, source.ID)
	if err != nil || current.State != calls.Active || current.ReplacedByID != uuid.Nil {
		t.Fatal("busy third ended source", err)
	}
	var slots int
	if err = pool.QueryRow(t.Context(), `SELECT count(*) FROM call_user_slots WHERE call_id=$1`, source.ID).Scan(&slots); err != nil || slots != 2 {
		t.Fatalf("source slots changed %d %v", slots, err)
	}
}

func TestCallGroupAdmissionDeadlineAndMemberRemovalAreAuthoritative(t *testing.T) {
	r := sfuRepo(t)
	a, b, c := sfuPeer(t, r), sfuPeer(t, r), sfuPeer(t, r)
	room := groupRoom(t, a, b, c)
	call := sfuCommand(t, r, a, calls.Command{Action: "start_group", RoomID: room, Mode: "audio"})
	sfuCommand(t, r, b, calls.Command{Action: "accept", ID: call.ID})
	for _, p := range []callPeer{a, b} {
		call = sfuCommand(t, r, p, calls.Command{Action: "ready", ID: call.ID, Generation: 1, Ready: true})
	}
	call = sfuCommand(t, r, c, calls.Command{Action: "accept", ID: call.ID})
	binding := calls.MediaBinding{CallID: call.ID, EndpointID: c.ref.ID, Epoch: c.ref.Epoch, Generation: call.Generation, MediaVersion: call.Participant(c.actor.UserID).MediaVersion}
	if _, err := r.MediaEndpoint(t.Context(), binding, time.Now().UTC()); err != nil {
		t.Fatal("current selected participant denied", err)
	}
	if _, err := r.MediaEndpoint(t.Context(), binding, call.Participant(c.actor.UserID).MediaDeadline); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("setup deadline bypassed: %v", err)
	}
	sfuCommand(t, r, c, calls.Command{Action: "ready", ID: call.ID, Generation: 1, Ready: true})
	if err := r.RemoveMember(t.Context(), room, c.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.MediaEndpoint(t.Context(), binding, time.Now().UTC()); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("removed room member admitted: %v", err)
	}
	current, err := r.GetCall(t.Context(), a.actor.UserID, call.ID)
	if err != nil || current.State != calls.Active || !current.Participant(c.actor.UserID).Retired() {
		t.Fatal("membership and call ACL diverged", err)
	}
	var events int
	if err = pool.QueryRow(t.Context(), `SELECT count(*) FROM call_outbox WHERE call_id=$1 AND user_id=$2 AND version=$3`, call.ID, c.actor.UserID, current.Version).Scan(&events); err != nil || events != 1 {
		t.Fatalf("missing durable remove event %d %v", events, err)
	}
}
