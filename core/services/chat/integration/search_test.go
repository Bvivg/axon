//go:build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func TestSearchIgnoresCaseAndTakesWildcardsLiterally(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "search")

	plain := say(t, r, room.ID, owner, "Release Notes are out", "s-1")
	percent := say(t, r, room.ID, owner, "coverage is 50% now", "s-2")
	say(t, r, room.ID, owner, "coverage is 500 now", "s-3")
	underscore := say(t, r, room.ID, owner, "see snake_case", "s-4")
	say(t, r, room.ID, owner, "see snakeXcase", "s-5")

	for query, want := range map[string]uuid.UUID{
		"release notes": plain.ID,
		"50%":           percent.ID,
		"snake_":        underscore.ID,
	} {
		found, err := r.SearchMessages(t.Context(), room.ID, owner, query, domain.MaxSearchResults)
		if err != nil {
			t.Fatalf("SearchMessages(%q): %v", query, err)
		}
		if len(found) != 1 || found[0].ID != want {
			t.Errorf("SearchMessages(%q) = %v, want exactly one match", query, bodiesOf(found))
		}
	}

	cyrillic := say(t, r, room.ID, owner, "Привет, МИР", "s-6")
	found, err := r.SearchMessages(t.Context(), room.ID, owner, "мир", domain.MaxSearchResults)
	if err != nil || len(found) != 1 || found[0].ID != cyrillic.ID {
		t.Fatalf("SearchMessages(мир) = %v, %v, want the cyrillic message", bodiesOf(found), err)
	}
}

func TestSearchSkipsDeletedSystemAndClearedMessages(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "search")
	other, _ := newRoom(t, r, "elsewhere")

	say(t, r, room.ID, owner, "deploy before the clear", "c-1")
	if err := r.HideRoom(t.Context(), room.ID, owner); err != nil {
		t.Fatalf("HideRoom: %v", err)
	}

	kept := say(t, r, room.ID, owner, "deploy after the clear", "c-2")
	gone := say(t, r, room.ID, owner, "deploy, deleted", "c-3")
	if _, err := r.DeleteMessage(t.Context(), gone.ID); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	if _, _, err := r.AppendMessage(t.Context(), domain.Message{
		ID: uuid.New(), RoomID: room.ID, AuthorID: owner, Body: "deploy group renamed",
		Kind: domain.MessageKindSystem, Payload: []byte(`{"event":"renamed"}`),
	}); err != nil {
		t.Fatalf("AppendMessage(system): %v", err)
	}
	say(t, r, other.ID, owner, "deploy in another room", "c-4")

	found, err := r.SearchMessages(t.Context(), room.ID, owner, "deploy", domain.MaxSearchResults)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(found) != 1 || found[0].ID != kept.ID {
		t.Fatalf("found %v, want only the message after the clear", bodiesOf(found))
	}
}

func TestSearchReturnsTheNewestMatchesUpToTheLimit(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "search")

	for i := range 60 {
		say(t, r, room.ID, owner, fmt.Sprintf("note %d", i), fmt.Sprintf("n-%d", i))
	}

	found, err := r.SearchMessages(t.Context(), room.ID, owner, "note", domain.MaxSearchResults)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(found) != domain.MaxSearchResults {
		t.Fatalf("found %d, want %d", len(found), domain.MaxSearchResults)
	}
	if found[0].Body != "note 59" || found[len(found)-1].Body != "note 10" {
		t.Fatalf("found %q … %q, want note 59 … note 10", found[0].Body, found[len(found)-1].Body)
	}
}

func bodiesOf(messages []domain.Message) []string {
	out := make([]string, 0, len(messages))
	for _, m := range messages {
		out = append(out, m.Body)
	}
	return out
}
