//go:build integration

package integration

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
)

func say(t *testing.T, r *repository.Repository, roomID, author uuid.UUID, body, clientID string) domain.Message {
	t.Helper()

	m, _, err := r.AppendMessage(t.Context(), domain.Message{
		ID:       uuid.New(),
		RoomID:   roomID,
		AuthorID: author,
		Body:     body,
		ClientID: clientID,
	})
	if err != nil {
		t.Fatalf("AppendMessage(%q): %v", body, err)
	}
	return m
}

func TestPositionsAreThePerRoomSequence(t *testing.T) {
	r := newRepo(t)

	first, owner := newRoom(t, r, "first")
	second, other := newRoom(t, r, "second")

	for i, want := range []int64{1, 2, 3} {
		if got := say(t, r, first.ID, owner, fmt.Sprintf("message %d", i), "").Seq; got != want {
			t.Errorf("message %d in the first room got position %d, want %d", i, got, want)
		}
	}

	if got := say(t, r, second.ID, other, "hello", "").Seq; got != 1 {
		t.Errorf("the first message in a second room got position %d, want 1", got)
	}
}

func TestAppendingToAMissingRoomIsRefused(t *testing.T) {
	r := newRepo(t)

	_, _, err := r.AppendMessage(t.Context(), domain.Message{
		ID:       uuid.New(),
		RoomID:   uuid.New(),
		AuthorID: uuid.New(),
		Body:     "into the void",
	})
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("err = %v, want ErrRoomNotFound", err)
	}
}

func TestAResendReturnsTheMessageItAlreadyWrote(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "general")
	const clientID = "client-message-1"

	first, duplicate, err := r.AppendMessage(t.Context(), domain.Message{
		ID: uuid.New(), RoomID: room.ID, AuthorID: owner, Body: "hello", ClientID: clientID,
	})
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	if duplicate {
		t.Error("a first send was reported as a duplicate")
	}

	again, duplicate, err := r.AppendMessage(t.Context(), domain.Message{
		ID: uuid.New(), RoomID: room.ID, AuthorID: owner, Body: "hello", ClientID: clientID,
	})
	if err != nil {
		t.Fatalf("resend: %v", err)
	}
	if !duplicate {
		t.Error("a resend was written as a new message")
	}
	if again.ID != first.ID || again.Seq != first.Seq {
		t.Errorf("resend returned %s at %d, want the original %s at %d",
			again.ID, again.Seq, first.ID, first.Seq)
	}

	messages, _, err := r.ListMessages(t.Context(), room.ID, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("the room holds %d messages after a resend, want 1", len(messages))
	}
}

func TestTheSameClientIDFromTwoPeopleIsTwoMessages(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "general")
	other := uuid.New()

	say(t, r, room.ID, owner, "mine", "message-1")
	say(t, r, room.ID, other, "theirs", "message-1")

	messages, _, err := r.ListMessages(t.Context(), room.ID, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("the room holds %d messages, want both", len(messages))
	}
}

func TestMessagesWithoutAClientIDDoNotCollide(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "general")

	say(t, r, room.ID, owner, "first", "")
	say(t, r, room.ID, owner, "second", "")

	messages, _, err := r.ListMessages(t.Context(), room.ID, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("the room holds %d messages, want 2", len(messages))
	}
}

func TestConcurrentSendersLeaveNoGap(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "busy")

	const senders = 16

	var wg sync.WaitGroup
	errs := make(chan error, senders)

	for i := range senders {
		wg.Add(1)
		go func() {
			defer wg.Done()

			_, _, err := r.AppendMessage(t.Context(), domain.Message{
				ID:       uuid.New(),
				RoomID:   room.ID,
				AuthorID: owner,
				Body:     fmt.Sprintf("message %d", i),
			})
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent send: %v", err)
	}

	messages, _, err := r.ListMessages(t.Context(), room.ID, domain.Page{Limit: senders + 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != senders {
		t.Fatalf("the room holds %d messages, want %d", len(messages), senders)
	}

	for i, m := range messages {
		if want := int64(i + 1); m.Seq != want {
			t.Fatalf("message %d sits at position %d, want %d — the sequence has a hole in it",
				i, m.Seq, want)
		}
	}
}

func TestPagingReadsBothWaysInOneOrder(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "general")
	for i := range 10 {
		say(t, r, room.ID, owner, fmt.Sprintf("message %d", i), "")
	}

	page, more, err := r.ListMessages(t.Context(), room.ID, domain.Page{Limit: 4})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if !more {
		t.Error("has_more was false with six messages still behind the page")
	}
	if got := seqs(page); !equal(got, []int64{7, 8, 9, 10}) {
		t.Errorf("the most recent page = %v, want the last four in order", got)
	}

	page, more, err = r.ListMessages(t.Context(), room.ID, domain.Page{Limit: 4, BeforeSeq: 7})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if !more {
		t.Error("has_more was false with two messages still behind the page")
	}
	if got := seqs(page); !equal(got, []int64{3, 4, 5, 6}) {
		t.Errorf("the page before 7 = %v, want 3..6", got)
	}

	page, more, err = r.ListMessages(t.Context(), room.ID, domain.Page{Limit: 4, AfterSeq: 8})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if more {
		t.Error("has_more was true at the end of the room")
	}
	if got := seqs(page); !equal(got, []int64{9, 10}) {
		t.Errorf("catching up from 8 = %v, want 9 and 10", got)
	}
}

func seqs(messages []domain.Message) []int64 {
	out := make([]int64, len(messages))
	for i, m := range messages {
		out[i] = m.Seq
	}
	return out
}

func equal(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
