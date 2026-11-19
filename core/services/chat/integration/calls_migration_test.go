//go:build integration

package integration

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
)

func migrationRepo(t *testing.T) (*repository.Repository, callPeer, callPeer, calls.Call) {
	t.Helper()
	newRepo(t)
	r := repository.NewWithOptions(pool, repository.Options{SFU: true, Migration: true})
	a, b := sfuPeer(t, r), sfuPeer(t, r)
	for _, p := range []callPeer{a, b} {
		if err := r.SetEndpointMigration(t.Context(), p.actor, p.ref, true, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	call := migrationCommand(t, r, a, calls.Command{Action: "start", RoomID: directCallRoom(t, a, b), Mode: "video"})
	migrationCommand(t, r, b, calls.Command{Action: "accept", ID: call.ID})
	for _, p := range []callPeer{a, b} {
		call = migrationCommand(t, r, p, calls.Command{Action: "ready", ID: call.ID, Generation: 1, Ready: true})
	}
	call = migrationCommand(t, r, a, calls.Command{Action: "migrate", ID: call.ID, Generation: 1})
	return r, a, b, call
}
func migrationCommand(t *testing.T, r *repository.Repository, p callPeer, cmd calls.Command) calls.Call {
	t.Helper()
	cmd.SFUCapable = true
	cmd.MigrationCapable = true
	cmd.EnableSFU = true
	cmd.EnableMigration = true
	if cmd.Action == "migration_ready" || cmd.Action == "migration_switched" || cmd.Action == "migration_clean" || cmd.Action == "migration_abort_clean" || cmd.Action == "migration_failed" {
		c, err := r.GetCall(t.Context(), p.actor.UserID, cmd.ID)
		if err != nil {
			t.Fatal(err)
		}
		cmd.MigrationID = c.Migration.ID
		cmd.Generation = c.Migration.TargetGeneration
		cmd.MediaVersion = c.Migration.Peer(p.actor.UserID).MediaVersion
	}
	c, err := callCommand(r, p, cmd)
	if err != nil {
		t.Fatalf("%s: %v", cmd.Action, err)
	}
	return c
}
func TestCallMigrationAtomicAuditAdmissionAndCleanup(t *testing.T) {
	r, a, b, call := migrationRepo(t)
	id, start := call.ID, *call.StartedAt
	binding := calls.MediaBinding{CallID: id, EndpointID: a.ref.ID, Epoch: a.ref.Epoch, Generation: 2, MediaVersion: 1}
	if _, err := r.MediaEndpoint(t.Context(), binding, time.Now().UTC()); err != nil {
		t.Fatal("candidate denied", err)
	}
	if ended, err := r.MediaRoomEnded(t.Context(), "call_"+id.String()); err != nil || ended {
		t.Fatal("reconciler would delete preparing candidate", err)
	}
	var attempts int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM call_transports WHERE call_id=$1 AND state<>'ended'`, id).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("parallel attempts=%d err=%v", attempts, err)
	}
	for _, action := range []string{"migration_ready", "migration_switched", "migration_clean"} {
		for _, p := range []callPeer{a, b} {
			call = migrationCommand(t, r, p, calls.Command{Action: action, ID: id})
		}
	}
	if call.ID != id || !call.StartedAt.Equal(start) || call.TransportKind() != "sfu" || call.Generation != 2 || call.Migration.Phase != "completed" {
		t.Fatal("business call changed")
	}
	var phase string
	if err := pool.QueryRow(t.Context(), `SELECT phase FROM call_migrations WHERE call_id=$1`, id).Scan(&phase); err != nil || phase != "completed" {
		t.Fatalf("audit=%s %v", phase, err)
	}
	var slots int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM call_user_slots WHERE call_id=$1`, id).Scan(&slots); err != nil || slots != 2 {
		t.Fatalf("migration released active slots=%d %v", slots, err)
	}
	sfuCommand(t, r, a, calls.Command{Action: "end", ID: id})
	for _, p := range []callPeer{a, b} {
		sfuCommand(t, r, p, calls.Command{Action: "cleanup", ID: id, Generation: 2})
	}
	if _, err := r.MediaEndpoint(t.Context(), binding, time.Now().UTC()); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("terminal admission=%v", err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM call_user_slots WHERE call_id=$1`, id).Scan(&slots); err != nil || slots != 0 {
		t.Fatalf("final slots=%d %v", slots, err)
	}
}
func TestCallMigrationAbortFencesCandidateAndRetainsSource(t *testing.T) {
	r, a, b, call := migrationRepo(t)
	id := call.ID
	migrationCommand(t, r, a, calls.Command{Action: "migration_failed", ID: id})
	for _, p := range []callPeer{a, b} {
		call = migrationCommand(t, r, p, calls.Command{Action: "migration_abort_clean", ID: id, Ready: true})
	}
	if call.State != calls.Active || call.TransportKind() != "p2p" || call.Generation != 1 {
		t.Fatal("rollback lost live source")
	}
	if _, err := r.MediaEndpoint(t.Context(), calls.MediaBinding{CallID: id, EndpointID: a.ref.ID, Epoch: 1, Generation: 2, MediaVersion: 1}, time.Now().UTC()); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("aborted candidate admitted=%v", err)
	}
	if ended, err := r.MediaRoomEnded(t.Context(), "call_"+id.String()); err != nil || !ended {
		t.Fatal("aborted room retained", err)
	}
	call = migrationCommand(t, r, a, calls.Command{Action: "migrate", ID: id, Generation: 1})
	if call.Migration.TargetGeneration != 3 {
		t.Fatal("generation reused")
	}
	var audits int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM call_migrations WHERE call_id=$1`, id).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("audit lost=%d %v", audits, err)
	}
	if _, err := r.GetCall(t.Context(), uuid.New(), id); !errors.Is(err, calls.ErrNotFound) {
		t.Fatal("outsider got migration", err)
	}
}

func TestCallMigrationSchema15CompatibilityAndHistoryGuard(t *testing.T) {
	newRepo(t)
	down, err := os.ReadFile("../migrations/000016_call_migrations.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../migrations/000016_call_migrations.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(t.Context(), string(down)); err != nil {
		t.Fatal(err)
	}
	r := repository.NewWithOptions(pool, repository.Options{SFU: true})
	a, b := sfuPeer(t, r), sfuPeer(t, r)
	c := sfuCommand(t, r, a, calls.Command{Action: "start", RoomID: directCallRoom(t, a, b), Mode: "audio"})
	sfuCommand(t, r, b, calls.Command{Action: "accept", ID: c.ID})
	if _, err = pool.Exec(t.Context(), string(up)); err != nil {
		t.Fatal(err)
	}
	if _, err = r.GetCall(t.Context(), a.actor.UserID, c.ID); err != nil {
		t.Fatal("schema15 history lost", err)
	}
	migrationRepo(t)
	if _, err = pool.Exec(t.Context(), string(down)); err == nil {
		t.Fatal("down destroyed migration audit")
	}
	var audits int
	if err = pool.QueryRow(t.Context(), `SELECT count(*) FROM call_migrations`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audit=%d %v", audits, err)
	}
}

func TestCallMigrationRechecksActualPeerCapability(t *testing.T) {
	r, a, b, c := migrationRepo(t)
	migrationCommand(t, r, a, calls.Command{Action: "migration_failed", ID: c.ID})
	for _, peer := range []callPeer{a, b} {
		migrationCommand(t, r, peer, calls.Command{Action: "migration_abort_clean", ID: c.ID, Ready: true})
	}
	if err := r.SetEndpointMigration(t.Context(), b.actor, b.ref, false, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_, err := callCommand(r, a, calls.Command{Action: "migrate", ID: c.ID, Generation: 1, EnableMigration: true, EnableSFU: true})
	if !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("stale document capability accepted=%v", err)
	}
	current, err := r.GetCall(t.Context(), a.actor.UserID, c.ID)
	if err != nil || current.Migration.TargetGeneration != 2 || current.Migration.Phase != "aborted" {
		t.Fatal("failed admission created a new operation", err)
	}
}

func TestCallMigrationConcurrentEndFencesLastSwitch(t *testing.T) {
	r, a, b, c := migrationRepo(t)
	for _, peer := range []callPeer{a, b} {
		c = migrationCommand(t, r, peer, calls.Command{Action: "migration_ready", ID: c.ID})
	}
	c = migrationCommand(t, r, a, calls.Command{Action: "migration_switched", ID: c.ID})
	results := make(chan error, 2)
	start := make(chan struct{})
	go func() {
		<-start
		_, err := callCommand(r, b, calls.Command{Action: "migration_switched", ID: c.ID, MigrationID: c.Migration.ID, Generation: 2, MediaVersion: 1})
		results <- err
	}()
	go func() {
		<-start
		_, err := callCommand(r, a, calls.Command{Action: "end", ID: c.ID})
		results <- err
	}()
	close(start)
	for range 2 {
		if err := <-results; err != nil && !errors.Is(err, calls.ErrStale) {
			t.Fatal(err)
		}
	}
	current, err := r.GetCall(t.Context(), a.actor.UserID, c.ID)
	if err != nil || current.State != calls.Ended {
		t.Fatal("switch resurrected ended call", err)
	}
	if _, err = r.MediaEndpoint(t.Context(), calls.MediaBinding{CallID: c.ID, EndpointID: b.ref.ID, Epoch: b.ref.Epoch, Generation: 2, MediaVersion: 1}, time.Now().UTC()); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("post-End admission=%v", err)
	}
}
