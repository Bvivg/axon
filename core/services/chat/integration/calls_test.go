//go:build integration

package integration

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/services/chat/internal/pubsub"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

type callPeer struct {
	actor calls.Actor
	ref   calls.EndpointRef
}

func callUser(t *testing.T, r *repository.Repository) callPeer {
	t.Helper()
	actor := calls.Actor{UserID: uuid.New(), FamilyID: uuid.New()}
	proof := strings.Repeat("x", 32) + uuid.NewString()
	endpoint, err := r.RegisterEndpoint(t.Context(), actor, nil, proof, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return callPeer{actor: actor, ref: calls.EndpointRef{ID: endpoint.ID, Epoch: endpoint.Epoch, Proof: proof}}
}
func directCallRoom(t *testing.T, a, b callPeer) uuid.UUID {
	t.Helper()
	users := []string{a.actor.UserID.String(), b.actor.UserID.String()}
	sort.Strings(users)
	id := uuid.New()
	if _, err := pool.Exec(t.Context(), `INSERT INTO rooms(id,name,created_by,kind,direct_user_min,direct_user_max) VALUES($1,'Direct',$2,'direct',$3,$4)`, id, a.actor.UserID, users[0], users[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO room_members(room_id,user_id) VALUES($1,$2),($1,$3)`, id, a.actor.UserID, b.actor.UserID); err != nil {
		t.Fatal(err)
	}
	return id
}
func callCommand(r *repository.Repository, peer callPeer, cmd calls.Command) (calls.Call, error) {
	cmd.Endpoint = peer.ref
	if cmd.RequestID == uuid.Nil {
		cmd.RequestID = uuid.New()
	}
	if cmd.StartsDirect() && cmd.ID == uuid.Nil {
		cmd.ID = uuid.New()
	}
	now := time.Now().UTC()
	return r.Mutate(context.Background(), peer.actor, cmd, now, func(c *calls.Call) (bool, error) { return calls.Apply(c, peer.actor.UserID, cmd, now) })
}
func TestCallPushClaimReceiptsAndEndpointACK(t *testing.T) {
	r := newRepo(t)
	a, b := callUser(t, r), callUser(t, r)
	call, err := callCommand(r, a, calls.Command{Action: "start", RoomID: directCallRoom(t, a, b), Mode: "audio"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	jobs, err := r.DueCallPush(t.Context(), now)
	if err != nil || len(jobs) != 1 || jobs[0].Event.UserID != b.actor.UserID || jobs[0].Event.Call.ID != call.ID {
		t.Fatal("callee push claim", jobs, err)
	}
	if again, err := r.DueCallPush(t.Context(), now); err != nil || len(again) != 0 {
		t.Fatal("claim duplicated", again, err)
	}
	if err := r.RecordCallPush(t.Context(), jobs[0].Event.ID, "subscription-hash", false); err != nil {
		t.Fatal(err)
	}
	retry, err := r.DueCallPush(t.Context(), now.Add(4*time.Second))
	if err != nil || len(retry) != 1 || !retry[0].Receipts["subscription-hash"] {
		t.Fatal("lost push receipt", retry, err)
	}
	if err := r.Acknowledge(t.Context(), b.actor, b.ref, jobs[0].Event.ID, now); err != nil {
		t.Fatal(err)
	}
	if due, err := r.DueCallPush(t.Context(), now.Add(8*time.Second)); err != nil || len(due) != 0 {
		t.Fatal("ACK did not suppress push", due, err)
	}
	if due, err := r.DueCallPush(t.Context(), now.Add(31*time.Second)); err != nil || len(due) != 0 {
		t.Fatal("expired push retried", due, err)
	}
}

func TestCallsLifecycleIdempotencyAndEndpointACK(t *testing.T) {
	r := newRepo(t)
	a, b := callUser(t, r), callUser(t, r)
	room := directCallRoom(t, a, b)
	key := uuid.New()
	start := calls.Command{Action: "start", RoomID: room, Mode: "audio", RequestID: key}
	call, err := callCommand(r, a, start)
	if err != nil {
		t.Fatal(err)
	}
	again, err := callCommand(r, a, start)
	if err != nil || again.ID != call.ID {
		t.Fatalf("duplicate start: %v", err)
	}
	altered := start
	altered.Mode = "video"
	if _, err := callCommand(r, a, altered); !errors.Is(err, calls.ErrConflict) {
		t.Fatalf("fingerprint: %v", err)
	}
	if _, err := r.GetCall(t.Context(), uuid.New(), call.ID); !errors.Is(err, calls.ErrNotFound) {
		t.Fatalf("outsider history: %v", err)
	}
	pending, err := r.Pending(t.Context(), a.actor, a.ref, time.Now())
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending: %d %v", len(pending), err)
	}
	if err := r.Acknowledge(t.Context(), b.actor, b.ref, pending[0].ID, time.Now()); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("foreign ACK: %v", err)
	}
	if err := r.Acknowledge(t.Context(), a.actor, a.ref, pending[0].ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	remaining, err := r.Pending(t.Context(), a.actor, a.ref, time.Now())
	if err != nil || len(remaining) != 0 {
		t.Fatal("ACK not durable", err)
	}
	secondProof := strings.Repeat("y", 32)
	endpoint, err := r.RegisterEndpoint(t.Context(), a.actor, nil, secondProof, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	remaining, err = r.Pending(t.Context(), a.actor, calls.EndpointRef{ID: endpoint.ID, Epoch: 1, Proof: secondProof}, time.Now())
	if err != nil || len(remaining) != 1 {
		t.Fatal("one endpoint ACK hid another endpoint event", err)
	}
	call, err = callCommand(r, b, calls.Command{Action: "accept", ID: call.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range []callPeer{a, b} {
		call, err = callCommand(r, peer, calls.Command{Action: "ready", ID: call.ID, Generation: 1, Ready: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	if call.State != calls.Active {
		t.Fatal("not active")
	}
	call, err = callCommand(r, a, calls.Command{Action: "end", ID: call.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range []callPeer{a, b} {
		if _, err := callCommand(r, peer, calls.Command{Action: "cleanup", ID: call.ID, Generation: 1}); err != nil {
			t.Fatal(err)
		}
	}
	var slots int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM call_user_slots WHERE call_id=$1`, call.ID).Scan(&slots); err != nil || slots != 0 {
		t.Fatalf("slots=%d %v", slots, err)
	}
}
func TestConcurrentCallsReserveUsersOnce(t *testing.T) {
	r := newRepo(t)
	a, b, c := callUser(t, r), callUser(t, r), callUser(t, r)
	ab, bc := directCallRoom(t, a, b), directCallRoom(t, b, c)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, room := range []uuid.UUID{ab, bc} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := callCommand(r, b, calls.Command{Action: "start", RoomID: room, Mode: "audio"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes, busy := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, calls.ErrBusy) {
			busy++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || busy != 1 {
		t.Fatalf("success=%d busy=%d", successes, busy)
	}
	var count int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM calls`).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed call committed", count, err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM call_outbox`).Scan(&count); err != nil || count != 2 {
		t.Fatal("partial outbox", count, err)
	}
}
func TestOnlyOneEndpointAccepts(t *testing.T) {
	r := newRepo(t)
	a, b := callUser(t, r), callUser(t, r)
	room := directCallRoom(t, a, b)
	call, err := callCommand(r, a, calls.Command{Action: "start", RoomID: room, Mode: "audio"})
	if err != nil {
		t.Fatal(err)
	}
	proof := strings.Repeat("z", 32)
	endpoint, err := r.RegisterEndpoint(t.Context(), b.actor, nil, proof, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	other := callPeer{actor: b.actor, ref: calls.EndpointRef{ID: endpoint.ID, Epoch: 1, Proof: proof}}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, peer := range []callPeer{b, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := callCommand(r, peer, calls.Command{Action: "accept", ID: call.ID})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, calls.ErrConflict) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("accepted %d times", successes)
	}
}
func TestOutboxSurvivesAbandonedClaimAndRedisSubscribeIsReady(t *testing.T) {
	r := newRepo(t)
	a, b := callUser(t, r), callUser(t, r)
	room := directCallRoom(t, a, b)
	if _, err := callCommand(r, a, calls.Command{Action: "start", RoomID: room, Mode: "audio"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	first, err := r.DueEvents(t.Context(), now)
	if err != nil || len(first) != 2 {
		t.Fatal("claim", len(first), err)
	}
	recovered, err := r.DueEvents(t.Context(), now.Add(2*time.Second))
	if err != nil || len(recovered) != 2 {
		t.Fatal("recovery", len(recovered), err)
	}
	bus, err := pubsub.NewRedis(cache, logger.Discard())
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ch, stop, err := bus.SubscribeUserSignals(ctx, a.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	publisher := pubsub.CallsPublisher{Bus: bus}
	for _, event := range recovered {
		if err := publisher.PublishCall(ctx, event.UserID, event.ID); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case event := <-ch:
		if event.Kind != pubsub.SignalCallState || event.EventID == uuid.Nil {
			t.Fatal("bad signal")
		}
	case <-ctx.Done():
		t.Fatal("initial signal was lost")
	}
	endpoint, err := r.RegisterEndpoint(t.Context(), a.actor, &a.ref, a.ref.Proof, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Epoch != 2 {
		t.Fatal("epoch did not advance")
	}
	if _, err := r.Pending(t.Context(), a.actor, a.ref, time.Now()); !errors.Is(err, calls.ErrStale) {
		t.Fatalf("stale endpoint: %v", err)
	}
}

func TestCallsEndpointConnectionAndFamilyAreFenced(t *testing.T) {
	r := newRepo(t)
	a := callUser(t, r)
	now := time.Now()
	first, second := uuid.New(), uuid.New()
	if err := r.BindEndpoint(t.Context(), a.actor, a.ref, first, now); err != nil {
		t.Fatal(err)
	}
	if err := r.BindEndpoint(t.Context(), a.actor, a.ref, second, now); !errors.Is(err, calls.ErrStale) {
		t.Fatalf("second socket accepted same epoch: %v", err)
	}
	otherFamily := a.actor
	otherFamily.FamilyID = uuid.New()
	if _, err := r.RegisterEndpoint(t.Context(), otherFamily, &a.ref, a.ref.Proof, now); !errors.Is(err, calls.ErrForbidden) {
		t.Fatalf("different family resumed endpoint: %v", err)
	}
	endpoint, err := r.RegisterEndpoint(t.Context(), a.actor, &a.ref, a.ref.Proof, now)
	if err != nil {
		t.Fatal(err)
	}
	fresh := a.ref
	fresh.Epoch = endpoint.Epoch
	if err := r.BindEndpoint(t.Context(), a.actor, fresh, second, now); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Endpoint(t.Context(), a.actor, a.ref, now, true); !errors.Is(err, calls.ErrStale) {
		t.Fatalf("old socket authority survived: %v", err)
	}
}

func TestCallsExpiryRechecksAConcurrentLeaseRenewal(t *testing.T) {
	r := newRepo(t)
	a, b := callUser(t, r), callUser(t, r)
	room := directCallRoom(t, a, b)
	call, err := callCommand(r, a, calls.Command{Action: "start", RoomID: room, Mode: "audio"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = callCommand(r, b, calls.Command{Action: "accept", ID: call.ID}); err != nil {
		t.Fatal(err)
	}
	for _, peer := range []callPeer{a, b} {
		if _, err := callCommand(r, peer, calls.Command{Action: "ready", ID: call.ID, Generation: 1, Ready: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(t.Context(), `UPDATE call_endpoints SET lease_expires_at=now()-interval '1 second' WHERE id=$1`, a.ref.ID); err != nil {
		t.Fatal(err)
	}
	commands, err := r.Expiring(t.Context(), time.Now())
	if err != nil || len(commands) != 1 {
		t.Fatal("expiry candidate", commands, err)
	}
	if _, err := r.RegisterEndpoint(t.Context(), a.actor, &a.ref, a.ref.Proof, time.Now()); err != nil {
		t.Fatal(err)
	}
	cmd := commands[0]
	now := time.Now()
	after, err := r.Mutate(t.Context(), calls.Actor{}, cmd, now, func(c *calls.Call) (bool, error) { return calls.Apply(c, uuid.Nil, cmd, now) })
	if err != nil || after.State != calls.Active {
		t.Fatal("stale expiry ended renewed call", after.State, err)
	}
}
