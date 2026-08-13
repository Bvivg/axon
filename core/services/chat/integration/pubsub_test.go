//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/pubsub"
)

func newBus(t *testing.T) *pubsub.Redis {
	t.Helper()

	bus, err := pubsub.NewRedis(cache, logger.Discard())
	if err != nil {
		t.Fatalf("pubsub.NewRedis: %v", err)
	}
	t.Cleanup(func() {
		if err := bus.Close(); err != nil {
			t.Errorf("close bus: %v", err)
		}
	})

	return bus
}

func newTestMessage(roomID uuid.UUID, seq int64, body string) domain.Message {
	return domain.Message{
		ID:       uuid.New(),
		RoomID:   roomID,
		AuthorID: uuid.New(),
		Body:     body,
		Seq:      seq,
		SentAt:   time.Now().UTC().Truncate(time.Millisecond),
		ClientID: "client-1",
	}
}

func receive(t *testing.T, ch <-chan domain.Message) domain.Message {
	t.Helper()

	select {
	case m, ok := <-ch:
		if !ok {
			t.Fatal("the subscription closed before a message arrived")
		}
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no message arrived")
		return domain.Message{}
	}
}

func TestAMessageCrossesBetweenInstances(t *testing.T) {
	writer, reader := newBus(t), newBus(t)

	roomID := uuid.New()

	messages, stop, err := reader.Subscribe(t.Context(), roomID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer stop()

	waitForSubscriber(t, roomID)

	sent := newTestMessage(roomID, 1, "across the wire")
	if err := writer.Publish(t.Context(), sent); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got := receive(t, messages)

	if got.ID != sent.ID || got.Seq != sent.Seq || got.Body != sent.Body {
		t.Errorf("received %+v, want %+v", got, sent)
	}
	if got.RoomID != sent.RoomID || got.AuthorID != sent.AuthorID {
		t.Errorf("the message changed hands: %+v", got)
	}

	if got.ClientID != sent.ClientID {
		t.Errorf("client id = %q, wanted it carried across", got.ClientID)
	}
	if !got.SentAt.Equal(sent.SentAt) {
		t.Errorf("sent_at = %s, want %s", got.SentAt, sent.SentAt)
	}
}

func TestAnInstanceHearsItsOwnMessage(t *testing.T) {
	bus := newBus(t)

	roomID := uuid.New()

	messages, stop, err := bus.Subscribe(t.Context(), roomID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer stop()

	waitForSubscriber(t, roomID)

	sent := newTestMessage(roomID, 1, "to myself")
	if err := bus.Publish(t.Context(), sent); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if got := receive(t, messages); got.ID != sent.ID {
		t.Errorf("received %s, want the message this instance published (%s)", got.ID, sent.ID)
	}
}

func TestTwoLocalListenersShareOneSubscription(t *testing.T) {
	bus := newBus(t)

	roomID := uuid.New()

	first, stopFirst, err := bus.Subscribe(t.Context(), roomID)
	if err != nil {
		t.Fatalf("first Subscribe: %v", err)
	}
	defer stopFirst()

	second, stopSecond, err := bus.Subscribe(t.Context(), roomID)
	if err != nil {
		t.Fatalf("second Subscribe: %v", err)
	}
	defer stopSecond()

	waitForSubscriber(t, roomID)

	sent := newTestMessage(roomID, 1, "to both")
	if err := bus.Publish(t.Context(), sent); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if got := receive(t, first); got.ID != sent.ID {
		t.Errorf("the first listener received %s", got.ID)
	}
	if got := receive(t, second); got.ID != sent.ID {
		t.Errorf("the second listener received %s", got.ID)
	}

	stopFirst()

	next := newTestMessage(roomID, 2, "still listening")
	if err := bus.Publish(t.Context(), next); err != nil {
		t.Fatalf("second Publish: %v", err)
	}

	if got := receive(t, second); got.ID != next.ID {
		t.Errorf("the remaining listener received %s, want %s", got.ID, next.ID)
	}
}

func TestStoppingTheLastListenerClosesTheChannel(t *testing.T) {
	bus := newBus(t)

	roomID := uuid.New()

	messages, stop, err := bus.Subscribe(t.Context(), roomID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	stop()

	select {
	case _, ok := <-messages:
		if ok {
			t.Error("a message arrived after the subscription was stopped")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the channel stayed open after the last listener left")
	}
}

func waitForSubscriber(t *testing.T, roomID uuid.UUID) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		counts, err := cache.PubSubNumSub(t.Context(), "chat:room:"+roomID.String()).Result()
		if err != nil {
			t.Fatalf("PUBSUB NUMSUB: %v", err)
		}
		if counts["chat:room:"+roomID.String()] > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("redis never reported a subscriber on the room's channel")
}
