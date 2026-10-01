//go:build integration

package integration

import (
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func TestAPushEndpointBelongsToWhoeverSubscribedItLast(t *testing.T) {
	r := newRepo(t)
	ada, bob := uuid.New(), uuid.New()
	shared := "https://push.example.test/" + uuid.NewString()
	own := "https://push.example.test/" + uuid.NewString()

	for _, s := range []domain.PushSubscription{
		{Endpoint: shared, UserID: ada, P256DH: "k1", Auth: "a1"},
		{Endpoint: own, UserID: ada, P256DH: "k2", Auth: "a2"},
		{Endpoint: shared, UserID: bob, P256DH: "k3", Auth: "a3"},
	} {
		if err := r.SavePushSubscription(t.Context(), s); err != nil {
			t.Fatalf("SavePushSubscription: %v", err)
		}
	}

	endpointsOf := func(user uuid.UUID) map[string]domain.PushSubscription {
		t.Helper()
		subs, err := r.PushSubscriptions(t.Context(), []uuid.UUID{user})
		if err != nil {
			t.Fatalf("PushSubscriptions: %v", err)
		}
		out := make(map[string]domain.PushSubscription, len(subs))
		for _, s := range subs {
			out[s.Endpoint] = s
		}
		return out
	}

	if got := endpointsOf(ada); len(got) != 1 || got[own].P256DH != "k2" {
		t.Fatalf("ada's endpoints = %+v, want only her own", got)
	}
	if got := endpointsOf(bob); len(got) != 1 || got[shared].P256DH != "k3" || got[shared].Auth != "a3" {
		t.Fatalf("bob's endpoints = %+v, want the shared one with his keys", got)
	}

	if err := r.DeletePushSubscription(t.Context(), ada, shared); err != nil {
		t.Fatalf("DeletePushSubscription: %v", err)
	}
	if got := endpointsOf(bob); len(got) != 1 {
		t.Fatalf("ada removed bob's endpoint: %+v", got)
	}

	if err := r.DropPushEndpoint(t.Context(), shared); err != nil {
		t.Fatalf("DropPushEndpoint: %v", err)
	}
	if got := endpointsOf(bob); len(got) != 0 {
		t.Fatalf("a dead endpoint survived: %+v", got)
	}
	if subs, err := r.PushSubscriptions(t.Context(), nil); err != nil || len(subs) != 0 {
		t.Fatalf("PushSubscriptions(nil) = %v, %v", subs, err)
	}
}
