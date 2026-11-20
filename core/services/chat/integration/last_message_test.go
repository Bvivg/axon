//go:build integration

package integration

import (
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func TestLastMessagesComeInOneQueryAndRespectAClearedHistory(t *testing.T) {
	r := newRepo(t)

	busy, owner := newRoom(t, r, "busy")
	quiet, _ := newRoom(t, r, "quiet")
	if _, err := r.AddMember(t.Context(), quiet.ID, domain.Member{UserID: owner, DisplayName: "Owner"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	cleared, _ := newRoom(t, r, "cleared")
	if _, err := r.AddMember(t.Context(), cleared.ID, domain.Member{UserID: owner, DisplayName: "Owner"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	say(t, r, busy.ID, owner, "first", "b-1")
	latest := say(t, r, busy.ID, owner, "second", "b-2")
	say(t, r, cleared.ID, owner, "gone for me", "c-1")
	if err := r.HideRoom(t.Context(), cleared.ID, owner); err != nil {
		t.Fatalf("HideRoom: %v", err)
	}

	last, err := r.LastMessages(t.Context(), []uuid.UUID{busy.ID, quiet.ID, cleared.ID}, owner)
	if err != nil {
		t.Fatalf("LastMessages: %v", err)
	}
	if m, ok := last[busy.ID]; !ok || m.ID != latest.ID {
		t.Errorf("busy room's last = %+v, want %q", m, latest.Body)
	}
	if _, ok := last[quiet.ID]; ok {
		t.Error("an empty room has a last message")
	}
	if _, ok := last[cleared.ID]; ok {
		t.Error("a cleared history still shows its last message")
	}
}
