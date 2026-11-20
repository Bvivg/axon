//go:build integration

package integration

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func TestRoomlessDirectCallIsAtomicAndParticipantScoped(t *testing.T) {
	r := sfuRepo(t)
	a, b, stranger := sfuPeer(t, r), sfuPeer(t, r), sfuPeer(t, r)
	cmd := calls.Command{Action: "start_direct", TargetID: b.actor.UserID, Mode: "video", RequestID: uuid.New()}
	type startResult struct {
		call calls.Call
		err  error
	}
	started := make(chan startResult, 2)
	for range 2 {
		go func() {
			call, err := callCommand(r, a, cmd)
			started <- startResult{call: call, err: err}
		}()
	}
	first, second := <-started, <-started
	if first.err != nil || second.err != nil || first.call.ID != second.call.ID {
		t.Fatalf("concurrent start did not deduplicate: %v, %v", first.err, second.err)
	}
	call := first.call
	if call.RoomID != uuid.Nil || call.State != calls.Ringing || len(call.Participants) != 2 {
		t.Fatalf("unexpected direct call: %+v", call)
	}
	if again := sfuCommand(t, r, a, cmd); again.ID != call.ID {
		t.Fatalf("duplicate call %s != %s", again.ID, call.ID)
	}
	cmd.TargetID = stranger.actor.UserID
	if _, err := callCommand(r, a, cmd); !errors.Is(err, calls.ErrConflict) {
		t.Fatalf("changed recipient accepted: %v", err)
	}
	if _, err := r.GetCall(t.Context(), stranger.actor.UserID, call.ID); !errors.Is(err, calls.ErrNotFound) {
		t.Fatalf("stranger read the call: %v", err)
	}
	for _, action := range []string{"accept", "end"} {
		if _, err := callCommand(r, stranger, calls.Command{Action: action, ID: call.ID}); !errors.Is(err, calls.ErrForbidden) {
			t.Fatalf("stranger executed %s: %v", action, err)
		}
	}
	if _, err := r.MediaEndpoint(t.Context(), calls.MediaBinding{CallID: call.ID, EndpointID: stranger.ref.ID, Epoch: stranger.ref.Epoch, Generation: 1}, time.Now().UTC()); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("stranger gained media admission: %v", err)
	}
	var rooms, messages, slots, events int
	var roomless bool
	err := pool.QueryRow(t.Context(), `SELECT room_id IS NULL,
 (SELECT count(*) FROM rooms), (SELECT count(*) FROM messages),
 (SELECT count(*) FROM call_user_slots WHERE call_id=$1),
 (SELECT count(*) FROM call_outbox WHERE call_id=$1) FROM calls WHERE id=$1`, call.ID).
		Scan(&roomless, &rooms, &messages, &slots, &events)
	if err != nil || !roomless || rooms != 0 || messages != 0 || slots != 2 || events != 2 {
		t.Fatalf("start side effects roomless=%v rooms=%d messages=%d slots=%d events=%d err=%v", roomless, rooms, messages, slots, events, err)
	}
	sfuCommand(t, r, b, calls.Command{Action: "decline", ID: call.ID})
	sfuCommand(t, r, a, calls.Command{Action: "cleanup", ID: call.ID, Generation: 1})
	for _, p := range []callPeer{a, b} {
		history, err := r.ListCalls(t.Context(), p.actor.UserID, uuid.Nil, 10)
		if err != nil || len(history) != 1 || history[0].ID != call.ID || history[0].EndReason != "declined" {
			t.Fatalf("declined call missing in history: %v", err)
		}
	}
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM call_user_slots WHERE call_id=$1`, call.ID).Scan(&slots); err != nil || slots != 0 {
		t.Fatalf("decline retained slots=%d: %v", slots, err)
	}
}

func TestRoomlessDirectOutcomesDoNotCreateChats(t *testing.T) {
	for _, reason := range []string{"cancelled", "missed", "connection_failed"} {
		t.Run(reason, func(t *testing.T) {
			r := sfuRepo(t)
			a, b := sfuPeer(t, r), sfuPeer(t, r)
			call := sfuCommand(t, r, a, calls.Command{Action: "start_direct", TargetID: b.actor.UserID, Mode: "audio"})
			at := call.Deadline.Add(time.Second)
			timeout := func(now time.Time) calls.Call {
				cmd := calls.Command{Action: "timeout", ID: call.ID}
				result, err := r.Mutate(t.Context(), calls.Actor{}, cmd, now, func(c *calls.Call) (bool, error) {
					return calls.Apply(c, uuid.Nil, cmd, now)
				})
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			switch reason {
			case "cancelled":
				call = sfuCommand(t, r, a, calls.Command{Action: "cancel", ID: call.ID})
			case "missed":
				call = timeout(at)
			case "connection_failed":
				sfuCommand(t, r, b, calls.Command{Action: "accept", ID: call.ID})
				var err error
				call, err = callCommand(r, a, calls.Command{Action: "ready", ID: call.ID, Generation: call.Generation})
				if err != nil {
					t.Fatal(err)
				}
			}
			if call.State != calls.Ended || call.EndReason != reason || call.RoomID != uuid.Nil {
				t.Fatalf("unexpected outcome state=%s reason=%s room=%s", call.State, call.EndReason, call.RoomID)
			}
			timeout(call.ReleaseAt.Add(time.Second))
			var rooms, messages, slots int
			err := pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM rooms), (SELECT count(*) FROM messages), (SELECT count(*) FROM call_user_slots WHERE call_id=$1)`, call.ID).
				Scan(&rooms, &messages, &slots)
			if err != nil || rooms != 0 || messages != 0 || slots != 0 {
				t.Fatalf("unexpected side effects rooms=%d messages=%d slots=%d err=%v", rooms, messages, slots, err)
			}
		})
	}
}
