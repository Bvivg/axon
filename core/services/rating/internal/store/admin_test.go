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
	"github.com/bvivg/axon/core/shared/pkg/adminquery"
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
		page, err := ratings.Changes(ctx, store.ChangeFilter{Cursor: cursor, Limit: 4})
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, page.Changes...)
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if len(seen) != 6 || seen[0].Session != sessions[2] || seen[5].Session != sessions[0] {
		t.Fatalf("paged %d changes, want 6 newest first: %+v", len(seen), seen)
	}

	chess, err := ratings.Changes(ctx, store.ChangeFilter{Game: "chess", User: &users[1], Limit: 10})
	if err != nil || len(chess.Changes) != 2 || chess.Total != nil || chess.Changes[0].User != users[1] || chess.Changes[0].RatingAfter <= chess.Changes[0].RatingBefore {
		t.Fatalf("chess changes of the winner = %+v, %v", chess, err)
	}

	if _, err := ratings.Changes(ctx, store.ChangeFilter{Cursor: "%%%", Limit: 10}); !errors.Is(err, store.ErrInvalidCursor) {
		t.Fatalf("broken cursor = %v", err)
	}

	losses, err := ratings.Changes(ctx, store.ChangeFilter{Limit: 10, List: adminquery.Request{Filter: "user = " + users[0].String() + " AND delta < 0"}})
	if err != nil || len(losses.Changes) != 3 || losses.Next != "" || losses.Changes[0].Session != sessions[2] {
		t.Fatalf("losses of the loser = %+v, %v", losses, err)
	}
	offset := int32(1)
	gains, err := ratings.Changes(ctx, store.ChangeFilter{List: adminquery.Request{
		Filter: "delta > 0 AND rating_after > 0 AND rating_before > 0 AND finished >= 2000-01-01", OrderBy: "finished", Offset: &offset, PageSize: 1,
	}})
	if err != nil || gains.Total == nil || *gains.Total != 3 || gains.Next != "" || len(gains.Changes) != 1 || gains.Changes[0].Session != sessions[1] || gains.Changes[0].User != users[1] {
		t.Fatalf("offset page of gains = %+v, %v", gains, err)
	}
	game, err := ratings.Changes(ctx, store.ChangeFilter{Game: "checkers", List: adminquery.Request{Filter: "session = " + sessions[2].String(), OrderBy: "delta desc", Offset: new(int32)}})
	if err != nil || *game.Total != 2 || game.Changes[0].User != users[1] || game.Changes[1].User != users[0] {
		t.Fatalf("one game by delta = %+v, %v", game, err)
	}
	var invalid *adminquery.Error
	if _, err := ratings.Changes(ctx, store.ChangeFilter{Limit: 10, List: adminquery.Request{OrderBy: "delta"}}); !errors.As(err, &invalid) || invalid.Field != "order_by" {
		t.Fatalf("order without offset = %v", err)
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
		page, err := ratings.Ratings(ctx, store.RatingFilter{Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, page.Ratings...)
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if len(seen) != 5 || len(want) != 5 {
		t.Fatalf("paged %d ratings of %d: %+v", len(seen), len(want), seen)
	}
	for i, row := range seen {
		if row.Game != want[i].Game || row.Player != want[i].Player || row.UpdatedAt.IsZero() {
			t.Fatalf("row %d = %+v, want %+v", i, row, want[i])
		}
	}

	mine, err := ratings.Ratings(ctx, store.RatingFilter{Game: "chess", User: &users[1], Limit: 10})
	if err != nil || mine.Next != "" || len(mine.Ratings) != 1 || mine.Ratings[0].Player.User != users[1] || mine.Ratings[0].Player.Games != 2 || mine.Ratings[0].Player.Wins != 2 {
		t.Fatalf("chess rating of the winner = %+v, %v", mine, err)
	}

	if _, err := ratings.Ratings(ctx, store.RatingFilter{Cursor: "%%%", Limit: 10}); !errors.Is(err, store.ErrInvalidCursor) {
		t.Fatalf("broken cursor = %v", err)
	}

	everywhere, err := ratings.Ratings(ctx, store.RatingFilter{Limit: 10, List: adminquery.Request{Filter: "user = " + users[0].String()}})
	if err != nil || len(everywhere.Ratings) != 2 || everywhere.Ratings[0].Game != "checkers" || everywhere.Ratings[1].Game != "chess" {
		t.Fatalf("ratings of one player = %+v, %v", everywhere, err)
	}
	busiest, err := ratings.Ratings(ctx, store.RatingFilter{Game: "chess", List: adminquery.Request{
		Filter: "games >= 1 AND rating > 0 AND updated >= 2000-01-01", OrderBy: "games desc, rating", Offset: new(int32), PageSize: 1,
	}})
	if err != nil || busiest.Total == nil || *busiest.Total != 3 || busiest.Next != "" || len(busiest.Ratings) != 1 || busiest.Ratings[0].Player.User != users[1] {
		t.Fatalf("busiest chess player = %+v, %v", busiest, err)
	}
}
