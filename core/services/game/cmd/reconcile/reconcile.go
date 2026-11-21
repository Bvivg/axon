package main

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

type outcome struct {
	Session string `json:"session"`
	Moves   int    `json:"moves"`
	Error   string `json:"error,omitempty"`
}

type projected struct {
	Status engine.Status
	Moves  int
}

type Kind string

const (
	KindLost      Kind = "lost"
	KindDiverged  Kind = "diverged"
	KindUnsettled Kind = "unsettled"
)

type Gap struct {
	Kind      Kind          `json:"kind"`
	Session   uuid.UUID     `json:"session"`
	Confirmed int           `json:"confirmed"`
	Journal   int           `json:"journal"`
	ReadModel int           `json:"read_model"`
	Status    engine.Status `json:"status,omitempty"`
}

const sampleLimit = 10

type Report struct {
	Matches        int            `json:"matches"`
	Completed      int            `json:"completed"`
	Settled        int            `json:"completed_and_settled"`
	ConfirmedMoves int            `json:"confirmed_moves"`
	JournalMoves   int            `json:"journal_moves"`
	ReadModelMoves int            `json:"read_model_moves"`
	Lost           int            `json:"sessions_with_lost_moves"`
	Diverged       int            `json:"sessions_diverged"`
	Unsettled      int            `json:"completed_unsettled"`
	ClientFailures map[string]int `json:"client_failures"`
	Samples        []Gap          `json:"samples"`
}

func (r *Report) record(kind Kind, gap Gap) {
	gap.Kind = kind
	switch kind {
	case KindLost:
		r.Lost++
	case KindDiverged:
		r.Diverged++
	case KindUnsettled:
		r.Unsettled++
	}
	if len(r.Samples) < sampleLimit {
		r.Samples = append(r.Samples, gap)
	}
}

func (r Report) Problems(minCompleted float64) []string {
	var out []string
	if r.Lost > 0 {
		out = append(out, fmt.Sprintf("%d sessions lost confirmed moves", r.Lost))
	}
	if r.Diverged > 0 {
		out = append(out, fmt.Sprintf("%d sessions differ between the journal and the read model", r.Diverged))
	}
	if r.Unsettled > 0 {
		out = append(out, fmt.Sprintf("%d completed sessions are not settled in the read model", r.Unsettled))
	}
	if r.Matches == 0 || float64(r.Completed) < minCompleted*float64(r.Matches) {
		out = append(out, fmt.Sprintf("only %d of %d matches completed, need %.0f%%", r.Completed, r.Matches, minCompleted*100))
	}
	return out
}

func terminal(status engine.Status) bool {
	return status == engine.StatusWin || status == engine.StatusDraw
}

func compare(results []outcome, logged map[uuid.UUID]int, model map[uuid.UUID]projected) Report {
	report := Report{Matches: len(results), ClientFailures: map[string]int{}, Samples: []Gap{}}
	for _, moves := range logged {
		report.JournalMoves += moves
	}
	for _, row := range model {
		report.ReadModelMoves += row.Moves
	}
	for _, res := range results {
		id, err := uuid.Parse(res.Session)
		switch {
		case res.Error != "":
			report.ClientFailures[res.Error]++
		case err != nil:
			report.ClientFailures["not started"]++
		}
		if err != nil {
			continue
		}
		completed := res.Error == ""
		report.ConfirmedMoves += res.Moves
		gap := Gap{Session: id, Confirmed: res.Moves, Journal: logged[id], ReadModel: model[id].Moves, Status: model[id].Status}
		if completed {
			report.Completed++
		}
		switch {
		case gap.Journal < gap.Confirmed || gap.ReadModel < gap.Confirmed:
			report.record(KindLost, gap)
		case completed && (gap.Journal != gap.Confirmed || gap.ReadModel != gap.Confirmed || !terminal(gap.Status)):
			report.record(KindUnsettled, gap)
		case completed:
			report.Settled++
		}
	}
	ids := make([]uuid.UUID, 0, len(logged)+len(model))
	for id := range logged {
		ids = append(ids, id)
	}
	for id := range model {
		if _, ok := logged[id]; !ok {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for _, id := range ids {
		if logged[id] != model[id].Moves {
			report.record(KindDiverged, Gap{Session: id, Journal: logged[id], ReadModel: model[id].Moves, Status: model[id].Status})
		}
	}
	return report
}

func journalMoves(ctx context.Context, log journal.Log, ends []int64) (map[uuid.UUID]int, error) {
	plies := map[uuid.UUID]map[int]bool{}
	for partition := range log.Partitions() {
		fence := journal.NewFence()
		err := log.Read(ctx, partition, 0, ends[partition], func(entry journal.Entry) error {
			if !fence.Admit(entry) {
				return nil
			}
			id, change := entry.Event.Session.ID, entry.Event.Change
			switch change.Fact {
			case session.FactMoved:
				if plies[id] == nil {
					plies[id] = map[int]bool{}
				}
				plies[id][change.Move.Ply] = true
			case session.FactUndone:
				delete(plies[id], change.UndonePly)
			case session.FactCancelled:
				delete(plies, id)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("reconcile: read partition %d: %w", partition, err)
		}
	}
	counts := make(map[uuid.UUID]int, len(plies))
	for id, set := range plies {
		counts[id] = len(set)
	}
	return counts, nil
}
