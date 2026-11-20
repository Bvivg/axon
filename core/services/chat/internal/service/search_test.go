package service_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func TestSearchFindsMatchesNewestFirstWithoutDeletedOnes(t *testing.T) {
	h := newHarness(t)
	room, owner := h.openRoom(t, "general")

	first := h.say(t, room.ID, owner, "Deploy on Friday")
	h.say(t, room.ID, owner, "lunch?")
	gone := h.say(t, room.ID, owner, "deploy rolled back")
	last := h.say(t, room.ID, owner, "the DEPLOY went fine")
	if _, err := h.svc.DeleteMessage(t.Context(), gone.ID, owner); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}

	found, err := h.svc.SearchMessages(t.Context(), room.ID, owner, "  deploy ")
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(found) != 2 || found[0].ID != last.ID || found[1].ID != first.ID {
		t.Fatalf("found %v, want the last and the first deploy messages", bodies(found))
	}
}

func TestSearchRefusesShortQueriesAndStrangers(t *testing.T) {
	h := newHarness(t)
	room, owner := h.openRoom(t, "general")
	h.say(t, room.ID, owner, "hello")

	if _, err := h.svc.SearchMessages(t.Context(), room.ID, owner, " h "); err == nil {
		t.Fatal("a one-letter query was accepted")
	} else if _, ok := domain.AsValidationError(err); !ok {
		t.Fatalf("short query: %v, want a validation error", err)
	}

	if _, err := h.svc.SearchMessages(t.Context(), room.ID, uuid.New(), "hello"); !errors.Is(err, domain.ErrNotAMember) {
		t.Fatalf("stranger: %v, want ErrNotAMember", err)
	}
}

func bodies(messages []domain.Message) []string {
	out := make([]string, 0, len(messages))
	for _, m := range messages {
		out = append(out, m.Body)
	}
	return out
}
