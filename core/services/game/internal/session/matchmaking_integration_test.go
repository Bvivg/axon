package session

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
)

var matchLock sync.Mutex

func queueTest(t *testing.T) (*Store, context.Context) {
	t.Helper()
	matchLock.Lock()
	t.Cleanup(matchLock.Unlock)
	store, ctx := testStore(t)
	if _, err := store.pool.Exec(ctx, `DELETE FROM game.match_queue`); err != nil {
		t.Fatal(err)
	}
	return store, ctx
}

func cleanupUsers(t *testing.T, store *Store, ctx context.Context, users ...uuid.UUID) {
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM game.match_queue WHERE user_id = ANY($1)`, users)
		_, _ = store.pool.Exec(ctx, `DELETE FROM game.rating_changes WHERE user_id = ANY($1)`, users)
		_, _ = store.pool.Exec(ctx, `DELETE FROM game.moves WHERE session_id IN (SELECT id FROM game.sessions WHERE creator_id = ANY($1) OR opponent_id = ANY($1))`, users)
		_, _ = store.pool.Exec(ctx, `DELETE FROM game.sessions WHERE creator_id = ANY($1) OR opponent_id = ANY($1)`, users)
		_, _ = store.pool.Exec(ctx, `DELETE FROM game.ratings WHERE user_id = ANY($1)`, users)
	})
}

func seedRating(t *testing.T, store *Store, ctx context.Context, user uuid.UUID, value int) {
	t.Helper()
	if _, err := store.pool.Exec(ctx, `INSERT INTO game.ratings (user_id, game, rating) VALUES ($1,$2,$3)`, user, tictactoe.Key, value); err != nil {
		t.Fatal(err)
	}
}

func matchedSession(t *testing.T, store *Store, ctx context.Context, a, b uuid.UUID) Session {
	t.Helper()
	for _, user := range []uuid.UUID{a, b} {
		if _, err := store.FindMatch(ctx, user, uuid.New(), tictactoe.Key); err != nil {
			t.Fatal(err)
		}
	}
	created, err := store.MatchRound(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("created %d sessions", len(created))
	}
	return created[0]
}

func ratingOf(t *testing.T, store *Store, ctx context.Context, user uuid.UUID) Rating {
	t.Helper()
	ratings, err := store.Ratings(ctx, tictactoe.Key, []uuid.UUID{user})
	if err != nil {
		t.Fatal(err)
	}
	return ratings[user]
}

func TestMatchmakingPairsAndSettlesOnce(t *testing.T) {
	store, ctx := queueTest(t)
	a, b := uuid.New(), uuid.New()
	cleanupUsers(t, store, ctx, a, b)

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			_, err := store.FindMatch(ctx, a, uuid.New(), tictactoe.Key)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var queued int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM game.match_queue WHERE user_id=$1`, a).Scan(&queued); err != nil || queued != 1 {
		t.Fatal("queue rows", queued, err)
	}
	game := matchedSession(t, store, ctx, a, b)
	if !game.Rated || game.Opponent == nil || game.Clock == nil || game.State.Status != engine.StatusInProgress {
		t.Fatal(game)
	}
	if again, err := store.MatchRound(ctx); err != nil || len(again) != 0 {
		t.Fatal("matched twice", again, err)
	}
	for _, user := range []uuid.UUID{a, b} {
		ticket, err := store.Ticket(ctx, user, tictactoe.Key)
		if err != nil || ticket.Session == nil || *ticket.Session != game.ID {
			t.Fatal("ticket", ticket, err)
		}
	}
	if _, err := store.Undo(ctx, game.ID, a, nil); !errors.Is(err, ErrRated) {
		t.Fatal("undo allowed in rated game", err)
	}

	x, o := game.Creator, *game.Opponent
	for index, cell := range []int{0, 3, 1, 4, 2} {
		player := x
		if index%2 == 1 {
			player = o
		}
		game = testMove(t, store, ctx, game, player, cell)
	}
	if game.State.Status != engine.StatusWin {
		t.Fatal(game.State)
	}
	if got := ratingOf(t, store, ctx, x); got.Rating != 1220 || got.Wins != 1 || got.Games != 1 {
		t.Fatal("winner", got)
	}
	if got := ratingOf(t, store, ctx, o); got.Rating != 1180 || got.Losses != 1 {
		t.Fatal("loser", got)
	}
	if _, _, err := store.Leave(ctx, game.ID, o); err != nil {
		t.Fatal(err)
	}
	changes, err := store.Changes(ctx, []uuid.UUID{game.ID})
	if err != nil || len(changes[game.ID]) != 2 {
		t.Fatal("changes", changes, err)
	}
	if got := ratingOf(t, store, ctx, x); got.Rating != 1220 || got.Games != 1 {
		t.Fatal("settled twice", got)
	}
	if _, err := store.Ticket(ctx, a, tictactoe.Key); !errors.Is(err, ErrNotSearching) {
		t.Fatal("finished ticket still visible", err)
	}
	ticket, err := store.FindMatch(ctx, a, uuid.New(), tictactoe.Key)
	if err != nil || ticket.Session != nil {
		t.Fatal("requeue after finished game", ticket, err)
	}
}

func TestMatchmakingPrefersCloseRatingAndCancels(t *testing.T) {
	store, ctx := queueTest(t)
	far, near, first := uuid.New(), uuid.New(), uuid.New()
	cleanupUsers(t, store, ctx, far, near, first)
	seedRating(t, store, ctx, first, 1200)
	seedRating(t, store, ctx, far, 1700)
	seedRating(t, store, ctx, near, 1260)
	for _, user := range []uuid.UUID{first, far, near} {
		if _, err := store.FindMatch(ctx, user, uuid.New(), tictactoe.Key); err != nil {
			t.Fatal(err)
		}
	}
	created, err := store.MatchRound(ctx)
	if err != nil || len(created) != 1 {
		t.Fatal(created, err)
	}
	if !created[0].HasPlayer(first) || !created[0].HasPlayer(near) {
		t.Fatal("paired distant players", created[0].Creator, created[0].Opponent)
	}
	if _, err := store.CancelMatch(ctx, far, tictactoe.Key); !errors.Is(err, ErrNotSearching) {
		t.Fatal(err)
	}
	if _, err := store.Ticket(ctx, far, tictactoe.Key); !errors.Is(err, ErrNotSearching) {
		t.Fatal("cancelled ticket remains", err)
	}
	if ticket, err := store.CancelMatch(ctx, near, tictactoe.Key); err != nil || ticket.Session == nil {
		t.Fatal("matched ticket cancelled", ticket, err)
	}
}

func TestRatedTimeoutAndResignSettle(t *testing.T) {
	store, ctx := queueTest(t)
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	cleanupUsers(t, store, ctx, a, b, c, d)

	timed := matchedSession(t, store, ctx, a, b)
	if _, err := store.pool.Exec(ctx, `UPDATE game.sessions SET clock = jsonb_set(clock, '{started_at}', to_jsonb(clock_timestamp() - interval '6 minutes')) WHERE id=$1`, timed.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	if got := ratingOf(t, store, ctx, timed.Creator); got.Rating != 1180 || got.Games != 1 {
		t.Fatal("timed out player", got)
	}
	if got := ratingOf(t, store, ctx, *timed.Opponent); got.Rating != 1220 {
		t.Fatal("waiting player", got)
	}

	resigned := matchedSession(t, store, ctx, c, d)
	loser := *resigned.Opponent
	game, cancelled, err := store.Leave(ctx, resigned.ID, loser)
	if err != nil || cancelled || !game.Resigned || game.State.Status != engine.StatusWin {
		t.Fatal(game, cancelled, err)
	}
	if got := ratingOf(t, store, ctx, loser); got.Rating != 1180 || got.Losses != 1 {
		t.Fatal("resigned player", got)
	}
	if _, _, err := store.Leave(ctx, resigned.ID, loser); err != nil {
		t.Fatal(err)
	}
	if got := ratingOf(t, store, ctx, loser); got.Games != 1 {
		t.Fatal("resign settled twice", got)
	}
}

func TestLeaveWaitingGameDeletesIt(t *testing.T) {
	store, ctx := testStore(t)
	owner, outsider := uuid.New(), uuid.New()
	cleanupUsers(t, store, ctx, owner)
	game, err := store.Create(ctx, owner, uuid.New(), tictactoe.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Leave(ctx, game.ID, outsider); !errors.Is(err, ErrNotFound) {
		t.Fatal("outsider left", err)
	}
	if _, cancelled, err := store.Leave(ctx, game.ID, owner); err != nil || !cancelled {
		t.Fatal(cancelled, err)
	}
	if _, err := store.Get(ctx, game.ID, owner); !errors.Is(err, ErrNotFound) {
		t.Fatal("waiting game survived", err)
	}
}
