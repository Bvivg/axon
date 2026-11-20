package outbox_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

type call struct {
	sql  string
	args []any
}

type recorder struct {
	calls []call
	err   error
}

func (r *recorder) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	r.calls = append(r.calls, call{sql, args})
	return pgconn.NewCommandTag("DELETE 3"), r.err
}

func TestWriteInsertsOneRowPerEventInTheCallersTransaction(t *testing.T) {
	r := &recorder{}
	id := uuid.New()
	finished := &eventsv1.GameFinished{EventId: id.String(), SessionId: "s", Game: "chess", Rated: true}
	if err := outbox.Write(context.Background(), r, "game.outbox", outbox.Event{ID: id, Topic: "game.finished.v1", AggregateID: "chess", Payload: finished}); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || !strings.HasPrefix(r.calls[0].sql, "INSERT INTO game.outbox ") || !strings.HasSuffix(r.calls[0].sql, "ON CONFLICT (id) DO NOTHING") {
		t.Fatal(r.calls)
	}
	args := r.calls[0].args
	if args[0] != id || args[1] != "game.finished.v1" || args[2] != "chess" || args[3] != "axon.events.v1.GameFinished" {
		t.Fatal(args)
	}
	decoded := &eventsv1.GameFinished{}
	if err := proto.Unmarshal(args[4].([]byte), decoded); err != nil || !proto.Equal(decoded, finished) {
		t.Fatal("payload does not round trip", err)
	}
}

func TestWriteRejectsBadInput(t *testing.T) {
	good := outbox.Event{ID: uuid.New(), Topic: "t", AggregateID: "a", Payload: &eventsv1.UserRegistered{}}
	for name, tt := range map[string]struct {
		table string
		event outbox.Event
	}{
		"table injection": {"auth.outbox; DROP TABLE x", good},
		"not an outbox":   {"auth.users", good},
		"no id":           {"auth.outbox", outbox.Event{Topic: "t", AggregateID: "a", Payload: good.Payload}},
		"no topic":        {"auth.outbox", outbox.Event{ID: uuid.New(), AggregateID: "a", Payload: good.Payload}},
		"no payload":      {"auth.outbox", outbox.Event{ID: uuid.New(), Topic: "t", AggregateID: "a"}},
	} {
		r := &recorder{}
		if err := outbox.Write(context.Background(), r, tt.table, tt.event); !errors.Is(err, outbox.ErrInvalid) || len(r.calls) != 0 {
			t.Errorf("%s: %v %v", name, err, r.calls)
		}
	}
}

func TestWritePropagatesInsertErrors(t *testing.T) {
	r := &recorder{err: errors.New("boom")}
	err := outbox.Write(context.Background(), r, "chat.outbox", outbox.Event{ID: uuid.New(), Topic: "t", AggregateID: "a", Payload: &eventsv1.ChatMessageSent{}})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatal(err)
	}
}

func TestCleanupDeletesOldRows(t *testing.T) {
	r := &recorder{}
	n, err := outbox.Cleanup(context.Background(), r, "chat.outbox", 24*time.Hour)
	if err != nil || n != 3 {
		t.Fatal(n, err)
	}
	if !strings.HasPrefix(r.calls[0].sql, "DELETE FROM chat.outbox ") || r.calls[0].args[0] != "24h0m0s" {
		t.Fatal(r.calls)
	}
}
