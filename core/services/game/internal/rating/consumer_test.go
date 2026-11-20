package rating_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/game/internal/rating"
	"github.com/bvivg/axon/core/services/game/internal/session"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

var (
	sessionID = uuid.MustParse("0b8a1c56-2f3a-4a11-9b7e-4f0d7a1e9c01")
	alice     = uuid.MustParse("1d6f2f43-9b35-4a6f-8f43-2a1c9d0e7b11")
	bob       = uuid.MustParse("2e7a3b54-ac46-4b70-9054-3b2dae1f8c22")
	changedAt = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	now       = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
)

func encode(t *testing.T, event *eventsv1.RatingChanged) []byte {
	t.Helper()
	raw, err := proto.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func valid() *eventsv1.RatingChanged {
	return &eventsv1.RatingChanged{
		EventId: uuid.NewString(), SessionId: sessionID.String(), Game: "chess", ChangedAt: timestamppb.New(changedAt),
		Changes: []*eventsv1.RatingChange{
			{UserId: alice.String(), Before: 1200, After: 1362, Games: 1, Wins: 1},
			{UserId: bob.String(), Before: 1200, After: 1038, Games: 1, Losses: 1},
		},
	}
}

func TestDecode(t *testing.T) {
	want := session.RatingUpdate{Session: sessionID, Game: "chess", At: changedAt, Players: []session.RatedPlayer{
		{User: alice, Before: 1200, After: 1362, Games: 1, Wins: 1},
		{User: bob, Before: 1200, After: 1038, Games: 1, Losses: 1},
	}}
	withoutSession, atNow := want, want
	withoutSession.Session, atNow.At = uuid.Nil, now
	tests := []struct {
		name   string
		mutate func(*eventsv1.RatingChanged)
		want   session.RatingUpdate
		err    bool
	}{
		{name: "settled game", mutate: func(*eventsv1.RatingChanged) {}, want: want},
		{name: "correction without a session", mutate: func(e *eventsv1.RatingChanged) { e.SessionId = "" }, want: withoutSession},
		{name: "missing time falls back to now", mutate: func(e *eventsv1.RatingChanged) { e.ChangedAt = nil }, want: atNow},
		{name: "no game", mutate: func(e *eventsv1.RatingChanged) { e.Game = "" }, err: true},
		{name: "no changes", mutate: func(e *eventsv1.RatingChanged) { e.Changes = nil }, err: true},
		{name: "bad session", mutate: func(e *eventsv1.RatingChanged) { e.SessionId = "nope" }, err: true},
		{name: "bad user", mutate: func(e *eventsv1.RatingChanged) { e.Changes[0].UserId = "nope" }, err: true},
		{name: "duplicate user", mutate: func(e *eventsv1.RatingChanged) { e.Changes[1].UserId = alice.String() }, err: true},
		{name: "negative counter", mutate: func(e *eventsv1.RatingChanged) { e.Changes[1].Losses = -1 }, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := valid()
			tt.mutate(event)
			got, err := rating.Decode(encode(t, event), now)
			if tt.err {
				if !errors.Is(err, rating.ErrMalformed) {
					t.Fatalf("Decode() error = %v, want ErrMalformed", err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Decode() = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
	if _, err := rating.Decode([]byte{0xff, 0xff}, now); !errors.Is(err, rating.ErrMalformed) {
		t.Fatalf("garbage decoded: %v", err)
	}
}

type fakeStore struct {
	updates []session.RatingUpdate
	err     error
}

func (f *fakeStore) ApplyRatings(_ context.Context, update session.RatingUpdate) error {
	f.updates = append(f.updates, update)
	return f.err
}

func TestHandle(t *testing.T) {
	good := encode(t, valid())
	typed := func(kind string) []kgo.RecordHeader {
		return []kgo.RecordHeader{{Key: "event_type", Value: []byte(kind)}}
	}
	tests := []struct {
		name    string
		record  *kgo.Record
		failure error
		applied int
		err     bool
	}{
		{name: "applies a rating change", record: &kgo.Record{Value: good, Headers: typed("axon.events.v1.RatingChanged")}, applied: 1},
		{name: "applies without a type header", record: &kgo.Record{Value: good}, applied: 1},
		{name: "skips another event type", record: &kgo.Record{Value: good, Headers: typed("axon.events.v1.GameFinished")}},
		{name: "skips a malformed record", record: &kgo.Record{Value: []byte{0xff}}},
		{name: "store failure is retried", record: &kgo.Record{Value: good}, failure: errors.New("db down"), applied: 1, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeStore{err: tt.failure}
			err := rating.NewConsumer(nil, rating.Topic, store, logger.Discard()).Handle(context.Background(), tt.record)
			if (err != nil) != tt.err || len(store.updates) != tt.applied {
				t.Fatalf("Handle() = %v with %d updates, want error %v and %d updates", err, len(store.updates), tt.err, tt.applied)
			}
		})
	}
}
