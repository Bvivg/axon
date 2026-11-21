package main

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

var (
	first  = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	second = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	third  = uuid.MustParse("00000000-0000-0000-0000-000000000003")
)

func TestCompare(t *testing.T) {
	won := func(moves int) projected { return projected{Status: engine.StatusWin, Moves: moves} }
	playing := func(moves int) projected { return projected{Status: engine.StatusInProgress, Moves: moves} }
	cases := []struct {
		name         string
		results      []outcome
		logged       map[uuid.UUID]int
		model        map[uuid.UUID]projected
		minCompleted float64
		lost         int
		diverged     int
		unsettled    int
		problems     int
		failures     map[string]int
	}{
		{
			name:     "every confirmed move is in the journal and the read model",
			results:  []outcome{{Session: first.String(), Moves: 5}, {Session: second.String(), Moves: 3, Error: "timeout"}},
			logged:   map[uuid.UUID]int{first: 5, second: 4},
			model:    map[uuid.UUID]projected{first: won(5), second: playing(4)},
			failures: map[string]int{"timeout": 1},
		},
		{
			name:     "the journal misses a confirmed move",
			results:  []outcome{{Session: first.String(), Moves: 5}},
			logged:   map[uuid.UUID]int{first: 4},
			model:    map[uuid.UUID]projected{first: won(5)},
			lost:     1,
			diverged: 1,
			problems: 2,
		},
		{
			name:     "the read model misses a confirmed move of a failed match",
			results:  []outcome{{Session: first.String(), Moves: 7}, {Session: second.String(), Moves: 3, Error: "socket closed: 1006"}},
			logged:   map[uuid.UUID]int{first: 7, second: 3},
			model:    map[uuid.UUID]projected{first: won(7), second: playing(2)},
			lost:     1,
			diverged: 1,
			problems: 2,
			failures: map[string]int{"socket closed: 1006": 1},
		},
		{
			name:      "a completed match is still in progress in the read model",
			results:   []outcome{{Session: first.String(), Moves: 5}},
			logged:    map[uuid.UUID]int{first: 5},
			model:     map[uuid.UUID]projected{first: playing(5)},
			unsettled: 1,
			problems:  1,
		},
		{
			name:      "a completed match has a move the client never saw",
			results:   []outcome{{Session: first.String(), Moves: 5}},
			logged:    map[uuid.UUID]int{first: 6},
			model:     map[uuid.UUID]projected{first: won(6)},
			unsettled: 1,
			problems:  1,
		},
		{
			name:     "the projection of a session outside the run diverges",
			results:  []outcome{{Session: first.String(), Moves: 5}},
			logged:   map[uuid.UUID]int{first: 5, third: 2},
			model:    map[uuid.UUID]projected{first: won(5)},
			diverged: 1,
			problems: 1,
		},
		{
			name:         "too few matches completed",
			results:      []outcome{{Session: first.String(), Moves: 5}, {Session: second.String(), Moves: 1, Error: "timeout"}, {}},
			logged:       map[uuid.UUID]int{first: 5, second: 1},
			model:        map[uuid.UUID]projected{first: won(5), second: playing(1)},
			minCompleted: 0.95,
			problems:     1,
			failures:     map[string]int{"timeout": 1, "not started": 1},
		},
		{
			name:         "a run without matches never passes",
			minCompleted: 0.95,
			problems:     1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			report := compare(c.results, c.logged, c.model)
			if report.Lost != c.lost || report.Diverged != c.diverged || report.Unsettled != c.unsettled {
				t.Fatalf("lost/diverged/unsettled = %d/%d/%d, want %d/%d/%d (samples %+v)",
					report.Lost, report.Diverged, report.Unsettled, c.lost, c.diverged, c.unsettled, report.Samples)
			}
			if got := report.Problems(c.minCompleted); len(got) != c.problems {
				t.Fatalf("problems = %q, want %d", got, c.problems)
			}
			if c.failures == nil {
				c.failures = map[string]int{}
			}
			if !maps.Equal(report.ClientFailures, c.failures) {
				t.Fatalf("client failures = %v, want %v", report.ClientFailures, c.failures)
			}
		})
	}
}

func TestCompareTotals(t *testing.T) {
	report := compare(
		[]outcome{{Session: first.String(), Moves: 5}, {Session: second.String(), Moves: 2, Error: "timeout"}},
		map[uuid.UUID]int{first: 5, second: 3},
		map[uuid.UUID]projected{first: {Status: engine.StatusDraw, Moves: 5}, second: {Status: engine.StatusInProgress, Moves: 3}},
	)
	if report.Matches != 2 || report.Completed != 1 || report.Settled != 1 {
		t.Fatalf("matches/completed/settled = %d/%d/%d", report.Matches, report.Completed, report.Settled)
	}
	if report.ConfirmedMoves != 7 || report.JournalMoves != 8 || report.ReadModelMoves != 8 {
		t.Fatalf("confirmed/journal/read model = %d/%d/%d", report.ConfirmedMoves, report.JournalMoves, report.ReadModelMoves)
	}
}

func TestJournalMovesFollowsTheProjectorRules(t *testing.T) {
	ctx := context.Background()
	log := journal.NewMemory(2)
	at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	revision := map[uuid.UUID]uint32{}
	event := func(id uuid.UUID, generation int64, change session.Change) journal.Entry {
		revision[id]++
		change.At = at
		return journal.Entry{Event: &journal.Event{
			Session: session.Session{ID: id, Revision: revision[id]}, Change: change, Generation: generation,
			Partial: change.Fact == session.FactMoved,
		}}
	}
	move := func(id uuid.UUID, generation int64, ply int) journal.Entry {
		return event(id, generation, session.Change{Fact: session.FactMoved, Move: &session.MoveRecord{Ply: ply}})
	}
	claim := func(generation int64) journal.Entry {
		return journal.Entry{Claim: &journal.Claim{Generation: generation, Owner: "game-1", At: at}}
	}
	write := func(id uuid.UUID, entries ...journal.Entry) {
		partition := journal.PartitionOf(id, log.Partitions())
		for _, entry := range entries {
			if _, err := log.Append(ctx, partition, entry); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(first,
		claim(1),
		event(first, 1, session.Change{Fact: session.FactCreated}),
		move(first, 1, 0), move(first, 1, 1), move(first, 1, 1), move(first, 1, 2), move(first, 1, 3),
		event(first, 1, session.Change{Fact: session.FactUndone, UndonePly: 3}),
		claim(2),
		move(first, 1, 3),
	)
	write(second,
		event(second, 2, session.Change{Fact: session.FactCreated}),
		move(second, 2, 0),
		event(second, 2, session.Change{Fact: session.FactCancelled}),
	)
	write(third,
		event(third, 2, session.Change{Fact: session.FactCreated}),
		move(third, 2, 0),
	)
	ends := make([]int64, log.Partitions())
	for partition := range log.Partitions() {
		end, err := log.End(ctx, partition)
		if err != nil {
			t.Fatal(err)
		}
		ends[partition] = end
	}
	got, err := journalMoves(ctx, log, ends)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID]int{first: 3, third: 1}
	if !maps.Equal(got, want) {
		t.Fatalf("journal moves = %v, want %v", got, want)
	}
}
