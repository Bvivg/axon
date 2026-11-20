//go:build integration

package integration

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func TestMediaAdmissionUsesCurrentMembershipSelectionLeaseAndGeneration(t *testing.T) {
	r := newRepo(t)
	a, b := callUser(t, r), callUser(t, r)
	roomID := directCallRoom(t, a, b)
	call, err := callCommand(r, a, calls.Command{Action: "start", RoomID: roomID, Mode: "audio"})
	if err != nil {
		t.Fatal(err)
	}
	call, err = callCommand(r, b, calls.Command{Action: "accept", ID: call.ID})
	if err != nil {
		t.Fatal(err)
	}
	binding := calls.MediaBinding{CallID: call.ID, EndpointID: a.ref.ID, Epoch: a.ref.Epoch, Generation: call.Generation, AllowP2PProbe: true}
	productionBinding := binding
	productionBinding.AllowP2PProbe = false
	if _, err := r.MediaEndpoint(t.Context(), productionBinding, time.Now().UTC()); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("P2P production admission allowed: %v", err)
	}
	endpoint, err := r.MediaEndpoint(t.Context(), binding, time.Now().UTC())
	if err != nil || endpoint.UserID != a.actor.UserID || endpoint.FamilyID != a.actor.FamilyID {
		t.Fatalf("valid admission endpoint=%+v err=%v", endpoint, err)
	}
	for _, mutate := range []func(*calls.MediaBinding){func(b *calls.MediaBinding) { b.Epoch++ }, func(b *calls.MediaBinding) { b.Generation++ }, func(b *calls.MediaBinding) { b.EndpointID = uuid.New() }, func(b *calls.MediaBinding) { b.CallID = uuid.New() }} {
		invalid := binding
		mutate(&invalid)
		if _, err := r.MediaEndpoint(t.Context(), invalid, time.Now().UTC()); !errors.Is(err, calls.ErrForbidden) {
			t.Fatalf("invalid binding admitted: %v", err)
		}
	}
	if _, err := pool.Exec(t.Context(), `DELETE FROM room_members WHERE room_id=$1 AND user_id=$2`, roomID, a.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.MediaEndpoint(t.Context(), binding, time.Now().UTC()); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("removed member admitted: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO room_members(room_id,user_id) VALUES($1,$2)`, roomID, a.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE call_endpoints SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, a.ref.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.MediaEndpoint(t.Context(), binding, time.Now().UTC()); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("expired endpoint admitted: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE call_endpoints SET lease_expires_at=clock_timestamp()+interval '30 seconds' WHERE id=$1`, a.ref.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = callCommand(r, a, calls.Command{Action: "end", ID: call.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.MediaEndpoint(t.Context(), binding, time.Now().UTC()); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("terminal call admitted: %v", err)
	}
}
