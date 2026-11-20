package service_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/service"
)

func TestForwardingSendsTheCommentThenCopiesToEveryRoom(t *testing.T) {
	h := newHarness(t)

	source, owner, _ := h.group(t, 1)
	first := h.say(t, source.ID, owner, "first")
	second := h.say(t, source.ID, owner, "second")

	targetA, _, _ := h.group(t, 1)
	targetB, _, _ := h.group(t, 1)
	for _, target := range []domain.Room{targetA, targetB} {
		if _, err := h.store.AddMember(t.Context(), target.ID, domain.Member{UserID: owner}); err != nil {
			t.Fatalf("AddMember: %v", err)
		}
	}

	in := service.ForwardInput{
		AuthorID:   owner,
		MessageIDs: []string{first.ID.String(), second.ID.String()},
		RoomIDs:    []string{targetA.ID.String(), targetB.ID.String()},
		Comment:    "  look at these  ",
		ClientID:   "batch-1",
	}
	sent, err := h.svc.ForwardMessages(t.Context(), in)
	if err != nil {
		t.Fatalf("ForwardMessages: %v", err)
	}
	if len(sent) != 6 {
		t.Fatalf("sent %d messages, want a comment and two copies in each of two rooms", len(sent))
	}

	for i, room := range []domain.Room{targetA, targetB} {
		batch := sent[i*3 : i*3+3]
		if batch[0].Message.RoomID != room.ID || batch[0].Message.Body != "look at these" || batch[0].Message.ForwardedFromID != nil {
			t.Errorf("room %d: first message = %+v, want the trimmed comment", i, batch[0].Message)
		}
		for j, want := range []domain.Message{first, second} {
			got := batch[j+1].Message
			if got.ForwardedFromID == nil || *got.ForwardedFromID != want.ID || got.Body != want.Body {
				t.Errorf("room %d copy %d = %+v, want a forward of %q", i, j, got, want.Body)
			}
		}
	}

	again, err := h.svc.ForwardMessages(t.Context(), in)
	if err != nil {
		t.Fatalf("ForwardMessages (retry): %v", err)
	}
	for _, item := range again {
		if !item.Duplicate {
			t.Fatalf("a retried batch sent %+v again", item.Message)
		}
	}
}

func TestForwardingRefusesWhatTheSenderCannotUse(t *testing.T) {
	h := newHarness(t)

	room, owner, members := h.group(t, 1)
	message := h.say(t, room.ID, owner, "mine")
	stranger, outsider, _ := h.group(t, 1)

	tooMany := make([]string, domain.MaxForwardMessages+1)
	for i := range tooMany {
		tooMany[i] = uuid.NewString()
	}

	invalid := func(err error) bool {
		_, ok := domain.AsValidationError(err)
		return ok
	}
	is := func(target error) func(error) bool {
		return func(err error) bool { return errors.Is(err, target) }
	}

	tests := map[string]struct {
		in   service.ForwardInput
		want func(error) bool
	}{
		"no messages": {
			in:   service.ForwardInput{AuthorID: owner, RoomIDs: []string{room.ID.String()}},
			want: invalid,
		},
		"too many messages": {
			in:   service.ForwardInput{AuthorID: owner, MessageIDs: tooMany, RoomIDs: []string{room.ID.String()}},
			want: invalid,
		},
		"a room the sender is not in": {
			in:   service.ForwardInput{AuthorID: owner, MessageIDs: []string{message.ID.String()}, RoomIDs: []string{stranger.ID.String()}},
			want: is(domain.ErrNotAMember),
		},
		"a message the sender cannot see": {
			in:   service.ForwardInput{AuthorID: outsider, MessageIDs: []string{message.ID.String()}, RoomIDs: []string{stranger.ID.String()}},
			want: is(domain.ErrInvalidForwardTarget),
		},
		"a comment that is too long": {
			in: service.ForwardInput{
				AuthorID: members[0], MessageIDs: []string{message.ID.String()}, RoomIDs: []string{room.ID.String()},
				Comment: strings.Repeat("x", domain.MaxMessageLength+1),
			},
			want: invalid,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			before := h.events.count()
			_, err := h.svc.ForwardMessages(t.Context(), tt.in)
			if !tt.want(err) {
				t.Fatalf("err = %v", err)
			}
			if after := h.events.count(); after != before {
				t.Fatalf("a refused forward still sent %d messages", after-before)
			}
		})
	}
}

func TestOnlyTheAuthorSeesWhoReadTheMessage(t *testing.T) {
	h := newHarness(t)

	room, owner, members := h.group(t, 2)
	message := h.say(t, room.ID, owner, "did you see this")

	if _, err := h.svc.MarkRead(t.Context(), room.ID, members[0], message.Seq+100); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	readers, err := h.svc.ListMessageReaders(t.Context(), message.ID, owner)
	if err != nil {
		t.Fatalf("ListMessageReaders: %v", err)
	}
	if len(readers) != 1 || readers[0].UserID != members[0] {
		t.Fatalf("readers = %+v, want only the member who read it", readers)
	}

	if _, err := h.svc.ListMessageReaders(t.Context(), message.ID, members[1]); !errors.Is(err, domain.ErrNotMessageAuthor) {
		t.Fatalf("a member who did not write it got err = %v, want ErrNotMessageAuthor", err)
	}
}

func TestGetRoomCountsWhatTheCallerHasNotRead(t *testing.T) {
	h := newHarness(t)

	room, owner, members := h.group(t, 1)
	for _, body := range []string{"one", "two", "three"} {
		h.say(t, room.ID, owner, body)
	}
	if _, err := h.svc.MarkRead(t.Context(), room.ID, members[0], 2); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	view, err := h.svc.GetRoom(t.Context(), room.ID, members[0])
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if view.Room.UnreadCount != 2 {
		t.Errorf("unread = %d, want the two messages after the cursor", view.Room.UnreadCount)
	}

	view, err = h.svc.GetRoom(t.Context(), room.ID, owner)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if view.Room.OthersReadSeq != 2 {
		t.Errorf("others read up to %d, want 2", view.Room.OthersReadSeq)
	}
}
