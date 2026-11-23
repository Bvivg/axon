package consumer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/rating/internal/consumer"
	"github.com/bvivg/axon/core/services/rating/internal/glicko"
	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	"github.com/bvivg/axon/core/services/rating/internal/store"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

type memoryStore struct {
	book  *ledger.Book
	err   error
	calls int
}

func (m *memoryStore) Apply(_ context.Context, game ledger.Game) (store.Applied, bool, error) {
	m.calls++
	if m.err != nil {
		return store.Applied{}, false, m.err
	}
	before := len(m.book.Changes)
	if !m.book.Apply(game) {
		return store.Applied{}, false, nil
	}
	changes := m.book.Changes[before:]
	return store.Applied{Game: game, Before: []ledger.Player{changes[0].Before, changes[1].Before}, After: []ledger.Player{changes[0].After, changes[1].After}}, true, nil
}

type memoryBoard struct {
	scores map[uuid.UUID]float64
	err    error
}

func (m *memoryBoard) Update(_ context.Context, _ string, players ...ledger.Player) error {
	if m.err != nil {
		return m.err
	}
	for _, player := range players {
		m.scores[player.User] = player.Rating.Rating
	}
	return nil
}

func record(t *testing.T, kind string, event *eventsv1.GameFinished) *kgo.Record {
	t.Helper()
	raw, err := proto.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return &kgo.Record{Value: raw, Headers: []kgo.RecordHeader{{Key: "event_type", Value: []byte(kind)}, {Key: "event_id", Value: []byte(event.EventId)}}}
}

func finished(rated bool) *eventsv1.GameFinished {
	return &eventsv1.GameFinished{
		EventId: uuid.NewString(), SessionId: uuid.NewString(), Game: "tictactoe", Players: []string{uuid.NewString(), uuid.NewString()},
		Outcome: eventsv1.GameFinished_OUTCOME_WIN, WinnerSeat: 1, FinishedAt: timestamppb.New(time.Now()), Rated: rated,
	}
}

const gameFinished = "axon.events.v1.GameFinished"

func TestHandle(t *testing.T) {
	rated := finished(true)
	tests := []struct {
		name      string
		records   []*kgo.Record
		storeErr  error
		boardErr  error
		wantErr   bool
		results   map[string]float64
		scores    int
		stale     bool
		applyCall int
	}{
		{name: "rated game settles and reaches the board", records: []*kgo.Record{record(t, gameFinished, rated)}, results: map[string]float64{"applied": 1}, scores: 2, applyCall: 1},
		{name: "redelivery is a duplicate", records: []*kgo.Record{record(t, gameFinished, rated), record(t, gameFinished, rated)}, results: map[string]float64{"applied": 1, "duplicate": 1}, scores: 2, applyCall: 2},
		{name: "unrated game is skipped", records: []*kgo.Record{record(t, gameFinished, finished(false))}, results: map[string]float64{"skipped": 1}},
		{name: "another event type is skipped", records: []*kgo.Record{record(t, "axon.events.v1.UserRegistered", rated)}, results: map[string]float64{"skipped": 1}},
		{name: "a poison record is skipped", records: []*kgo.Record{{Value: []byte{0xff, 0x01}}}, results: map[string]float64{"invalid": 1}},
		{name: "a store failure is retried", records: []*kgo.Record{record(t, gameFinished, rated)}, storeErr: errors.New("db down"), wantErr: true, applyCall: 1},
		{name: "a board failure schedules a resync", records: []*kgo.Record{record(t, gameFinished, rated)}, boardErr: errors.New("redis down"), results: map[string]float64{"applied": 1}, stale: true, applyCall: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := consumer.NewEventCounter(prometheus.NewRegistry())
			memory := &memoryStore{book: ledger.NewBook(glicko.Default), err: tt.storeErr}
			board := &memoryBoard{scores: map[uuid.UUID]float64{}, err: tt.boardErr}
			c := consumer.New(consumer.Config{Store: memory, Board: board, Log: logger.Discard(), Events: events})
			var err error
			for _, r := range tt.records {
				if err = c.Handle(context.Background(), r); err != nil {
					break
				}
			}
			if (err != nil) != tt.wantErr || memory.calls != tt.applyCall || len(board.scores) != tt.scores || c.Stale() != tt.stale {
				t.Fatalf("err=%v calls=%d scores=%d", err, memory.calls, len(board.scores))
			}
			for _, result := range []string{"applied", "duplicate", "skipped", "invalid"} {
				if got := testutil.ToFloat64(events.WithLabelValues(result)); got != tt.results[result] {
					t.Fatalf("%s = %v, want %v", result, got, tt.results[result])
				}
			}
		})
	}
}
