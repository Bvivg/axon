//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/tournament/internal/orchestrator"
	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	"github.com/bvivg/axon/core/shared/pkg/authz"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

func auditAs(name string) orchestrator.Audit {
	return func(_, after saga.Tournament) outbox.Event {
		return authz.NewAction(context.Background(), "tournament", name, "tournament", after.ID.String(), "integration").Event(uuid.New())
	}
}

func rename(name string) func(saga.Spec) saga.Spec {
	return func(spec saga.Spec) saga.Spec {
		spec.Name = name
		return spec
	}
}

func TestAdminEditsAreVersionedAndAuditedInTheSameTransaction(t *testing.T) {
	h := newHarness(t, time.Minute)
	ctx := context.Background()
	tour, ids := h.open(saga.Swiss, 8, 3, 3)
	if loaded := h.load(tour.ID); loaded.Version != 4 {
		t.Fatalf("version after create and three registrations = %d", loaded.Version)
	}

	_, after, err := h.saga.Revise(ctx, tour.ID, 4, rename("Renamed cup"), auditAs("tournament_updated"))
	if err != nil || after.Version != 5 || after.Spec.Name != "Renamed cup" {
		t.Fatalf("revise = %+v, %v", after.Spec, err)
	}
	if loaded := h.load(tour.ID); loaded.Spec.Name != "Renamed cup" || loaded.Version != 5 || h.published(tour, authz.Topic) != 1 {
		t.Fatalf("stored = %q v%d, audit rows %d", loaded.Spec.Name, loaded.Version, h.published(tour, authz.Topic))
	}

	var stale *orchestrator.StaleError
	if _, _, err := h.saga.Revise(ctx, tour.ID, 4, rename("Other name"), auditAs("tournament_updated")); !errors.As(err, &stale) || stale.Current != 5 {
		t.Fatalf("stale revise = %v", err)
	}
	if _, replay, err := h.saga.Revise(ctx, tour.ID, 4, rename("Renamed cup"), auditAs("tournament_updated")); err != nil || replay.Version != 5 {
		t.Fatalf("replay = v%d, %v", replay.Version, err)
	}
	var spec *saga.SpecError
	if _, _, err := h.saga.Revise(ctx, tour.ID, 5, func(s saga.Spec) saga.Spec { s.Capacity = 2; s.Rounds = 1; return s }, auditAs("tournament_updated")); !errors.As(err, &spec) {
		t.Fatalf("capacity below players = %v", err)
	}
	if h.published(tour, authz.Topic) != 1 || h.load(tour.ID).Version != 5 {
		t.Fatal("a rejected or replayed edit changed the tournament")
	}

	removed, err := h.saga.Remove(ctx, tour.ID, ids[1], auditAs("tournament_player_removed"))
	if err != nil || len(removed.Registered()) != 2 || removed.Version != 6 || h.published(tour, authz.Topic) != 2 {
		t.Fatalf("remove = %d players v%d, %v", len(removed.Registered()), removed.Version, err)
	}
	if _, err := h.saga.Remove(ctx, tour.ID, ids[1], auditAs("tournament_player_removed")); !errors.Is(err, saga.ErrNotRegistered) {
		t.Fatalf("second removal = %v", err)
	}

	if _, err := h.saga.Start(ctx, tour.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.saga.Revise(ctx, tour.ID, h.load(tour.ID).Version, rename("Too late"), auditAs("tournament_updated")); !errors.Is(err, saga.ErrClosed) {
		t.Fatalf("revise after start = %v", err)
	}
}
