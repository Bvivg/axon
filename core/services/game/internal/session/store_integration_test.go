package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
)

func cellData(cell int) json.RawMessage {
	raw, _ := json.Marshal(tictactoe.Cell{Row: cell / tictactoe.Size, Col: cell % tictactoe.Size})
	return raw
}

func testStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := os.Getenv("GAME_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GAME_TEST_DATABASE_URL is unset")
	}
	if !strings.Contains(dsn, "/axon_game_clock_test") {
		t.Fatal("requires isolated axon_game_clock_test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS game`); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('game.sessions') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	migrations := []string{"../../migrations/000001_sessions.up.sql", "../../migrations/000002_clocks_undo.up.sql"}
	if exists {
		migrations = nil
	}
	var rated bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('game.ratings') IS NOT NULL`).Scan(&rated); err != nil {
		t.Fatal(err)
	}
	if !rated {
		migrations = append(migrations, "../../migrations/000003_ratings_matchmaking.up.sql")
	}
	var drawn bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='game' AND table_name='sessions' AND column_name='agreed_draw')`).Scan(&drawn); err != nil {
		t.Fatal(err)
	}
	if !drawn {
		migrations = append(migrations, "../../migrations/000004_draw_offers.up.sql")
	}
	for _, path := range migrations {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	games := engine.NewRegistry()
	for _, def := range []engine.Definition{tictactoe.Definition(), chess.Definition()} {
		if err := games.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	return NewStore(pool, games), ctx
}

func testGame(t *testing.T, store *Store, ctx context.Context) (Session, uuid.UUID, uuid.UUID) {
	t.Helper()
	x, o := uuid.New(), uuid.New()
	game, err := store.Create(ctx, x, uuid.New(), tictactoe.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM game.moves WHERE session_id=$1`, game.ID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM game.sessions WHERE id=$1`, game.ID)
	})
	if game.Clock != nil || game.Revision != 0 {
		t.Fatal("waiting clock started")
	}
	game, err = store.Join(ctx, game.ID, o)
	if err != nil {
		t.Fatal(err)
	}
	if game.Revision != 1 || game.Clock == nil {
		t.Fatal(game)
	}
	return game, x, o
}

func testMove(t *testing.T, store *Store, ctx context.Context, game Session, user uuid.UUID, cell int) Session {
	t.Helper()
	result, err := store.Move(ctx, game.ID, user, game.State.Ply, tictactoe.Key, cellData(cell), &game.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision <= game.Revision {
		t.Fatal("revision did not advance")
	}
	return result
}

func TestStoreUndoAndRevision(t *testing.T) {
	store, ctx := testStore(t)
	game, x, o := testGame(t, store, ctx)
	game = testMove(t, store, ctx, game, x, 0)
	oldRevision := game.Revision
	game = testMove(t, store, ctx, game, o, 4)
	game, err := store.Undo(ctx, game.ID, o, &game.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if game.State.Ply != 1 || game.State.Turn != 1 || game.Revision != 4 {
		t.Fatal(game)
	}
	if _, err := store.Move(ctx, game.ID, o, 1, tictactoe.Key, cellData(8), &oldRevision); !errors.Is(err, engine.ErrStaleMove) {
		t.Fatal("stale command accepted", err)
	}
	if _, err := store.Undo(ctx, game.ID, x, &oldRevision); !errors.Is(err, engine.ErrStaleMove) {
		t.Fatal("stale undo accepted", err)
	}
	game = testMove(t, store, ctx, game, o, 8)
	if _, err := store.Undo(ctx, game.ID, uuid.New(), &game.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatal("outsider undo accepted", err)
	}
	read, err := store.Get(ctx, game.ID, x)
	if err != nil || read.Revision != game.Revision || read.State.Ply != 2 {
		t.Fatal(read, err)
	}
}

func TestStoreUndoCompletedAndLegacyGames(t *testing.T) {
	store, ctx := testStore(t)
	game, x, o := testGame(t, store, ctx)
	for ply, cell := range []int{0, 3, 1, 4, 2} {
		user := x
		if ply%2 == 1 {
			user = o
		}
		game = testMove(t, store, ctx, game, user, cell)
	}
	if !game.State.Over() {
		t.Fatal("not won")
	}
	game, err := store.Undo(ctx, game.ID, o, &game.Revision)
	if err != nil || game.State.Over() || game.State.Ply != 4 || game.State.Turn != 0 {
		t.Fatal(game, err)
	}
	game = testMove(t, store, ctx, game, x, 2)
	if _, err := store.pool.Exec(ctx, `UPDATE game.moves SET state_before=NULL,clock_before=NULL WHERE session_id=$1`, game.ID); err != nil {
		t.Fatal(err)
	}
	game, err = store.Undo(ctx, game.ID, x, &game.Revision)
	if err != nil || game.State.Ply != 4 || game.State.Over() {
		t.Fatal(game, err)
	}
	game = testMove(t, store, ctx, game, x, 2)
	if !game.State.Over() {
		t.Fatal("legacy undo lost earlier moves")
	}
}

func TestStoreTimeoutAndRestore(t *testing.T) {
	store, ctx := testStore(t)
	game, x, o := testGame(t, store, ctx)
	game = testMove(t, store, ctx, game, x, 0)
	clock := game.Clock
	clock.StartedAt = time.Now().Add(-6 * time.Minute)
	raw, _ := json.Marshal(clock)
	if _, err := store.pool.Exec(ctx, `UPDATE game.sessions SET clock=$2 WHERE id=$1`, game.ID, raw); err != nil {
		t.Fatal(err)
	}
	if err := store.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	game, err := store.Get(ctx, game.ID, o)
	if err != nil || !game.TimedOut || game.State.Winner != 0 || game.Clock.RemainingMS[1] != 0 {
		t.Fatal(game, err)
	}
	game, err = store.Undo(ctx, game.ID, x, &game.Revision)
	if err != nil || game.TimedOut || game.State.Over() || game.State.Ply != 0 || game.Clock.RemainingMS[1] != TimeLimitMS {
		t.Fatal(game, err)
	}
	game = testMove(t, store, ctx, game, x, 8)
	if game.State.Ply != 1 {
		t.Fatal(game)
	}
}

func TestStoreMoveAtDeadlineAndClockHistory(t *testing.T) {
	store, ctx := testStore(t)
	game, x, o := testGame(t, store, ctx)
	game.Clock.StartedAt = time.Now().Add(-2 * time.Second)
	raw, _ := json.Marshal(game.Clock)
	if _, err := store.pool.Exec(ctx, `UPDATE game.sessions SET clock=$2 WHERE id=$1`, game.ID, raw); err != nil {
		t.Fatal(err)
	}
	game = testMove(t, store, ctx, game, x, 0)
	charged := game.Clock.RemainingMS[0]
	if charged > 298000 || charged < 297000 {
		t.Fatal(charged)
	}
	game, err := store.Undo(ctx, game.ID, o, &game.Revision)
	if err != nil || game.Clock.RemainingMS[0] != charged {
		t.Fatal(game, err)
	}
	game.Clock.StartedAt = time.Now().Add(-6 * time.Minute)
	raw, _ = json.Marshal(game.Clock)
	if _, err := store.pool.Exec(ctx, `UPDATE game.sessions SET clock=$2 WHERE id=$1`, game.ID, raw); err != nil {
		t.Fatal(err)
	}
	game = testMove(t, store, ctx, game, x, 4)
	if !game.TimedOut || game.State.Ply != 0 || game.State.Winner != 1 {
		t.Fatal(game)
	}
}

func TestStoreConcurrentUndoAndMove(t *testing.T) {
	store, ctx := testStore(t)
	game, x, o := testGame(t, store, ctx)
	game = testMove(t, store, ctx, game, x, 0)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Go(func() { _, err := store.Undo(ctx, game.ID, x, &game.Revision); results <- err })
	wg.Go(func() {
		_, err := store.Move(ctx, game.ID, o, game.State.Ply, tictactoe.Key, cellData(4), &game.Revision)
		results <- err
	})
	wg.Wait()
	close(results)
	success, stale := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, engine.ErrStaleMove) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatal(success, stale)
	}
}
