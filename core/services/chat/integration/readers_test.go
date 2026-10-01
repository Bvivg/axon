//go:build integration

package integration

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func TestMarkReadStopsAtTheLatestMessageAndRecordsWhen(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "general")
	reader := uuid.New()
	if _, err := r.AddMember(t.Context(), room.ID, domain.Member{UserID: reader, DisplayName: "Reader"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	first := say(t, r, room.ID, owner, "one", "c-1")
	second := say(t, r, room.ID, owner, "two", "c-2")

	read, err := r.MarkRead(t.Context(), room.ID, reader, second.Seq+1000)
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if read != second.Seq {
		t.Fatalf("read up to %d, want the latest message %d", read, second.Seq)
	}

	if again, err := r.MarkRead(t.Context(), room.ID, reader, first.Seq); err != nil || again != second.Seq {
		t.Fatalf("MarkRead backwards = %d, %v; want it to stay at %d", again, err, second.Seq)
	}

	for _, m := range []domain.Message{first, second} {
		readers, err := r.Readers(t.Context(), room.ID, owner, m.Seq)
		if err != nil {
			t.Fatalf("Readers: %v", err)
		}
		if len(readers) != 1 || readers[0].UserID != reader || readers[0].ReadAt == nil {
			t.Fatalf("readers of #%d = %+v, want the reader with a time", m.Seq, readers)
		}
	}

	third := say(t, r, room.ID, owner, "three", "c-3")
	readers, err := r.Readers(t.Context(), room.ID, owner, third.Seq)
	if err != nil {
		t.Fatalf("Readers: %v", err)
	}
	if len(readers) != 0 {
		t.Fatalf("an unread message lists readers %+v", readers)
	}

	if _, err := r.MarkRead(t.Context(), room.ID, uuid.New(), third.Seq); !errors.Is(err, domain.ErrNotAMember) {
		t.Fatalf("a stranger marking read got err = %v, want ErrNotAMember", err)
	}
}

func TestConcurrentMarkReadsLeaveNoGapsOrOverlaps(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "general")
	reader := uuid.New()
	if _, err := r.AddMember(t.Context(), room.ID, domain.Member{UserID: reader, DisplayName: "Reader"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	var last domain.Message
	for i := range 20 {
		last = say(t, r, room.ID, owner, "message", "c-"+string(rune('a'+i)))
	}

	var wg sync.WaitGroup
	for seq := int64(1); seq <= last.Seq; seq++ {
		wg.Go(func() {
			if _, err := r.MarkRead(t.Context(), room.ID, reader, seq); err != nil {
				t.Errorf("MarkRead(%d): %v", seq, err)
			}
		})
	}
	wg.Wait()

	for seq := int64(1); seq <= last.Seq; seq++ {
		readers, err := r.Readers(t.Context(), room.ID, owner, seq)
		if err != nil {
			t.Fatalf("Readers: %v", err)
		}
		if len(readers) != 1 || readers[0].ReadAt == nil {
			t.Fatalf("message #%d readers = %+v, want exactly one timed read", seq, readers)
		}
	}
}
