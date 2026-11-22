package store_test

import (
	"bytes"
	"cmp"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	"github.com/bvivg/axon/core/services/rating/internal/store"
)

func TestRatingChangesPageNewestFirstAndFilter(t *testing.T) {
	ctx, _, ratings := setup(t)
	var sessions []uuid.UUID
	for i, key := range []string{"chess", "chess", "checkers"} {
		game := played(key, 0, 1, 1)
		game.FinishedAt = game.FinishedAt.Add(time.Duration(i) * time.Second)
		if _, ok, err := ratings.Apply(ctx, game); err != nil || !ok {
			t.Fatal("apply", ok, err)
		}
		sessions = append(sessions, game.Session)
	}

	var seen []store.Change
	cursor := ""
	for range 4 {
		page, next, err := ratings.Changes(ctx, store.ChangeFilter{Cursor: cursor, Limit: 4})
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, page...)
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != 6 || seen[0].Session != sessions[2] || seen[5].Session != sessions[0] {
		t.Fatalf("paged %d changes, want 6 newest first: %+v", len(seen), seen)
	}

	chess, _, err := ratings.Changes(ctx, store.ChangeFilter{Game: "chess", User: &users[1], Limit: 10})
	if err != nil || len(chess) != 2 || chess[0].User != users[1] || chess[0].RatingAfter <= chess[0].RatingBefore {
		t.Fatalf("chess changes of the winner = %+v, %v", chess, err)
	}

	if _, _, err := ratings.Changes(ctx, store.ChangeFilter{Cursor: "%%%", Limit: 10}); !errors.Is(err, store.ErrInvalidCursor) {
		t.Fatalf("broken cursor = %v", err)
	}
}

func TestRatingsPageByGameThenStrongestFirst(t *testing.T) {
	ctx, _, ratings := setup(t)
	for _, game := range []ledger.Game{played("chess", 0, 1, 1), played("chess", 1, 2, 0), played("checkers", 2, 0, 0)} {
		if _, ok, err := ratings.Apply(ctx, game); err != nil || !ok {
			t.Fatal("apply", ok, err)
		}
	}
	want, err := ratings.Standings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(want, func(a, b store.Standing) int {
		return cmp.Or(cmp.Compare(a.Game, b.Game), cmp.Compare(b.Player.Rating.Rating, a.Player.Rating.Rating), bytes.Compare(a.Player.User[:], b.Player.User[:]))
	})

	var seen []store.RatingRow
	cursor := ""
	for range 5 {
		page, next, err := ratings.Ratings(ctx, store.RatingFilter{Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, page...)
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != 5 || len(want) != 5 {
		t.Fatalf("paged %d ratings of %d: %+v", len(seen), len(want), seen)
	}
	for i, row := range seen {
		if row.Game != want[i].Game || row.Player != want[i].Player || row.UpdatedAt.IsZero() {
			t.Fatalf("row %d = %+v, want %+v", i, row, want[i])
		}
	}

	mine, next, err := ratings.Ratings(ctx, store.RatingFilter{Game: "chess", User: &users[1], Limit: 10})
	if err != nil || next != "" || len(mine) != 1 || mine[0].Player.User != users[1] || mine[0].Player.Games != 2 || mine[0].Player.Wins != 2 {
		t.Fatalf("chess rating of the winner = %+v, %q, %v", mine, next, err)
	}

	if _, _, err := ratings.Ratings(ctx, store.RatingFilter{Cursor: "%%%", Limit: 10}); !errors.Is(err, store.ErrInvalidCursor) {
		t.Fatalf("broken cursor = %v", err)
	}
}
