//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

func TestAdminListFiltersAndPagesSessions(t *testing.T) {
	ctx := t.Context()
	games := engine.NewRegistry()
	if err := games.Register(quoridor.Definition()); err != nil {
		t.Fatal(err)
	}
	rules := session.NewRules(games)
	store := session.NewStore(pool, games)
	tournament := uuid.New()

	var made []session.Session
	for range 3 {
		game := shuffleGame(t, ctx, rules, 4)
		if _, err := pool.Exec(ctx, `UPDATE game.sessions SET tournament_id = $1, updated_at = clock_timestamp() WHERE id = $2`, tournament, game.ID); err != nil {
			t.Fatal(err)
		}
		made = append(made, game)
	}
	waiting := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO game.sessions (id, game, creator_id, request_id, updated_at, revision, partition) VALUES ($1, 'quoridor', $2, $1, now(), 1, 0)`, uuid.New(), waiting); err != nil {
		t.Fatal(err)
	}

	var seen []uuid.UUID
	cursor := ""
	for range 3 {
		page, err := store.AdminList(ctx, session.AdminFilter{Tournament: &tournament, Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 3 {
			t.Errorf("total = %d, want 3", page.Total)
		}
		for _, s := range page.Sessions {
			seen = append(seen, s.ID)
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if len(seen) != 3 || seen[0] != made[2].ID || seen[2] != made[0].ID {
		t.Fatalf("paged %v, want the tournament games newest first", seen)
	}

	player := made[1].Creator
	live, err := store.AdminList(ctx, session.AdminFilter{Player: &player, State: session.AdminLive, Game: engine.Key("quoridor"), Limit: 10})
	if err != nil || live.Total != 1 || live.Sessions[0].ID != made[1].ID {
		t.Fatalf("live games of a player = %+v, %v", live, err)
	}
	finished, err := store.AdminList(ctx, session.AdminFilter{Player: &player, State: session.AdminFinished, Limit: 10})
	if err != nil || finished.Total != 0 {
		t.Fatalf("finished games of a player in progress = %+v, %v", finished, err)
	}
	open, err := store.AdminList(ctx, session.AdminFilter{Player: &waiting, State: session.AdminWaiting, Limit: 10})
	if err != nil || open.Total != 1 || open.Sessions[0].Opponent != nil {
		t.Fatalf("waiting games of their creator = %+v, %v", open, err)
	}
	if _, err := store.AdminList(ctx, session.AdminFilter{Cursor: "%%%", Limit: 10}); !errors.Is(err, session.ErrInvalidCursor) {
		t.Fatalf("broken cursor = %v", err)
	}

	others, err := store.AdminList(ctx, session.AdminFilter{Tournament: &tournament, Limit: 10, List: adminquery.Request{Filter: "player != " + player.String()}})
	if err != nil || others.Total != 2 || len(others.Sessions) != 2 || others.Sessions[0].ID != made[2].ID || others.Sessions[1].ID != made[0].ID {
		t.Fatalf("cursor list without one player = %+v, %v", others, err)
	}
	offset := int32(1)
	paged, err := store.AdminList(ctx, session.AdminFilter{Tournament: &tournament, Limit: 10, List: adminquery.Request{
		Filter: "status = live AND moves >= 4 AND rated = false AND game = quoridor", OrderBy: "updated", Offset: &offset, PageSize: 1,
	}})
	if err != nil || paged.Total != 3 || paged.Next != "" || len(paged.Sessions) != 1 || paged.Sessions[0].ID != made[1].ID {
		t.Fatalf("offset page = %+v, %v", paged, err)
	}
	none, err := store.AdminList(ctx, session.AdminFilter{Tournament: &tournament, List: adminquery.Request{Filter: "moves > 4 OR status = won", Offset: &offset}})
	if err != nil || none.Total != 0 || len(none.Sessions) != 0 {
		t.Fatalf("no game past ply 4 = %+v, %v", none, err)
	}
	var invalid *adminquery.Error
	if _, err := store.AdminList(ctx, session.AdminFilter{Limit: 10, List: adminquery.Request{OrderBy: "moves"}}); !errors.As(err, &invalid) || invalid.Field != "order_by" {
		t.Fatalf("order without offset = %v", err)
	}
	if _, err := store.AdminList(ctx, session.AdminFilter{Limit: 10, List: adminquery.Request{Filter: "creator_id = x", Offset: &offset}}); !errors.As(err, &invalid) || invalid.Field != "filter" {
		t.Fatalf("column name as a field = %v", err)
	}
}

func TestAdminSessionReadsTheRecordedMovesInOrder(t *testing.T) {
	ctx := t.Context()
	games := engine.NewRegistry()
	if err := games.Register(quoridor.Definition()); err != nil {
		t.Fatal(err)
	}
	rules := session.NewRules(games)
	store := session.NewStore(pool, games)
	game := shuffleGame(t, ctx, rules, 6)
	if _, err := pool.Exec(ctx, `INSERT INTO game.moves (session_id, ply, player_id, data, played_at) VALUES ($1, 7, $2, '{"move":"e3"}', now())`, game.ID, game.Creator); err != nil {
		t.Fatal(err)
	}

	got, moves, err := store.AdminSession(ctx, game.ID, session.AdminMoveLimit)
	if err != nil || got.ID != game.ID || got.Opponent == nil || len(moves) != 6 {
		t.Fatalf("admin session = %+v, %d moves, %v", got, len(moves), err)
	}
	for i, move := range moves {
		if move.Ply != i+1 || len(move.Data) == 0 || move.PlayedAt.IsZero() {
			t.Fatalf("move %d = %+v", i, move)
		}
	}
	if moves[0].Player != game.Creator || moves[1].Player != *game.Opponent {
		t.Fatalf("players of the first moves = %s, %s", moves[0].Player, moves[1].Player)
	}

	_, capped, err := store.AdminSession(ctx, game.ID, 4)
	if err != nil || len(capped) != 4 || capped[3].Ply != 4 {
		t.Fatalf("capped moves = %+v, %v", capped, err)
	}
	if _, _, err := store.AdminSession(ctx, uuid.New(), session.AdminMoveLimit); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("missing session = %v", err)
	}
}

func TestAdminQueueListsAndRemovesOnlyWaitingPlayers(t *testing.T) {
	ctx := t.Context()
	if _, err := pool.Exec(ctx, `DELETE FROM game.match_queue`); err != nil {
		t.Fatal(err)
	}
	games := engine.NewRegistry()
	for _, def := range []engine.Definition{quoridor.Definition(), tictactoe.Definition()} {
		if err := games.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	store := session.NewStore(pool, games)
	first, matched, other := uuid.New(), uuid.New(), uuid.New()
	for _, join := range []struct {
		user uuid.UUID
		game engine.Key
	}{{matched, quoridor.Key}, {first, quoridor.Key}, {other, tictactoe.Key}} {
		if _, err := store.FindMatch(ctx, join.user, uuid.New(), join.game); err != nil {
			t.Fatal(err)
		}
	}
	game := uuid.New()
	if _, err := pool.Exec(ctx, `UPDATE game.match_queue SET session_id = $1 WHERE user_id = $2`, game, matched); err != nil {
		t.Fatal(err)
	}

	queue := func(game engine.Key, limit int, list adminquery.Request) session.QueuePage {
		t.Helper()
		page, err := store.MatchQueue(ctx, session.QueueFilter{Game: game, Limit: limit, List: list})
		if err != nil {
			t.Fatalf("queue %q %+v: %v", game, list, err)
		}
		return page
	}
	all := queue("", session.AdminQueueLimit, adminquery.Request{}).Entries
	if len(all) != 3 {
		t.Fatalf("queue = %+v", all)
	}
	if all[0].User != first || all[1].User != other || all[2].User != matched || all[2].Session == nil || *all[2].Session != game ||
		all[0].Session != nil || all[0].Game != quoridor.Key || all[0].Rating == 0 || all[0].LastSeenAt.Before(all[0].JoinedAt) {
		t.Fatalf("queue order = %+v", all)
	}
	if quoridorQueue := queue(quoridor.Key, session.AdminQueueLimit, adminquery.Request{}); len(quoridorQueue.Entries) != 2 || quoridorQueue.Total != nil {
		t.Fatalf("quoridor queue = %+v", quoridorQueue)
	}
	if capped := queue("", 1, adminquery.Request{}).Entries; len(capped) != 1 || capped[0].User != first {
		t.Fatalf("capped queue = %+v", capped)
	}
	if waitingOnly := queue("", session.AdminQueueLimit, adminquery.Request{Filter: "matched = false AND user != " + other.String()}).Entries; len(waitingOnly) != 1 || waitingOnly[0].User != first {
		t.Fatalf("waiting quoridor players = %+v", waitingOnly)
	}
	offset := int32(1)
	paged := queue(quoridor.Key, session.AdminQueueLimit, adminquery.Request{Filter: "rating > 0 AND joined >= 2000-01-01", OrderBy: "joined desc", Offset: &offset, PageSize: 1})
	if paged.Total == nil || *paged.Total != 2 || len(paged.Entries) != 1 || paged.Entries[0].User != matched {
		t.Fatalf("offset queue page = %+v", paged)
	}
	if matchedOnly := queue("", session.AdminQueueLimit, adminquery.Request{Filter: "matched = true", Offset: new(int32)}); matchedOnly.Total == nil || *matchedOnly.Total != 1 || matchedOnly.Entries[0].User != matched {
		t.Fatalf("matched players = %+v", matchedOnly)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+session.MatchChannel); err != nil {
		t.Fatal(err)
	}
	if err := store.LeaveQueue(ctx, first, quoridor.Key); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	note, err := conn.Conn().WaitForNotification(wait)
	if err != nil || note.Payload != first.String() {
		t.Fatalf("match notification = %+v, %v", note, err)
	}
	if _, err := conn.Exec(ctx, "UNLISTEN *"); err != nil {
		t.Fatal(err)
	}

	if err := store.LeaveQueue(ctx, first, quoridor.Key); !errors.Is(err, session.ErrNotSearching) {
		t.Fatalf("second removal = %v", err)
	}
	if err := store.LeaveQueue(ctx, matched, quoridor.Key); !errors.Is(err, session.ErrNotSearching) {
		t.Fatalf("removal of a matched player = %v", err)
	}
	if err := store.LeaveQueue(ctx, other, quoridor.Key); !errors.Is(err, session.ErrNotSearching) {
		t.Fatalf("removal from a game the player is not queued for = %v", err)
	}
	left := queue("", session.AdminQueueLimit, adminquery.Request{}).Entries
	if len(left) != 2 || left[0].User != other || left[1].User != matched {
		t.Fatalf("queue after removal = %+v", left)
	}
}
