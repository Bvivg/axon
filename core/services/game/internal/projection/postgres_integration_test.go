package projection_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/projection"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

const partitions = 4

type env struct {
	ctx   context.Context
	pool  *pgxpool.Pool
	store *session.Store
	model *projection.Postgres
	log   *journal.Memory
	rules session.Rules
	skew  atomic.Int64
}

func setup(t *testing.T) *env {
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
	migrate(t, ctx, pool)
	for _, table := range []string{"game.match_queue", "game.rating_changes", "game.moves", "game.sessions", "game.ratings", "game.owners", "game.projector_offsets"} {
		if _, err := pool.Exec(ctx, `DELETE FROM `+table); err != nil {
			t.Fatal(err)
		}
	}
	games := engine.NewRegistry()
	for _, def := range []engine.Definition{tictactoe.Definition(), chess.Definition()} {
		if err := games.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	store := session.NewStore(pool, games)
	return &env{ctx: ctx, pool: pool, store: store, model: projection.New(store, session.NewRules(games)), log: journal.NewMemory(partitions), rules: session.NewRules(games)}
}

func migrate(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS game`); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		file  string
		query string
	}{
		{"000001_sessions", `SELECT to_regclass('game.sessions') IS NOT NULL`},
		{"000002_clocks_undo", `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='game' AND table_name='sessions' AND column_name='revision')`},
		{"000003_ratings_matchmaking", `SELECT to_regclass('game.ratings') IS NOT NULL`},
		{"000004_draw_offers", `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='game' AND table_name='sessions' AND column_name='agreed_draw')`},
		{"000005_event_log", `SELECT to_regclass('game.owners') IS NOT NULL`},
		{"000006_owner_lease", `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='game' AND table_name='owners' AND column_name='lease_until')`},
	}
	for _, check := range checks {
		var applied bool
		if err := pool.QueryRow(ctx, check.query).Scan(&applied); err != nil {
			t.Fatal(err)
		}
		if applied {
			continue
		}
		raw, err := os.ReadFile("../../migrations/" + check.file + ".up.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(raw)); err != nil {
			t.Fatal(check.file, err)
		}
	}
}

func (e *env) node(t *testing.T, id string) *owner.Owner {
	t.Helper()
	node := owner.New(owner.Config{ID: id, Address: id + ":8080", Log: e.log, Model: e.model, Rules: e.rules, Now: func() time.Time {
		return time.Now().Add(time.Duration(e.skew.Load()))
	}})
	for p := range int32(partitions) {
		if err := node.Assign(e.ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(node.RevokeAll)
	return node
}

func (e *env) project(t *testing.T) {
	t.Helper()
	batches := map[int32][]journal.Entry{}
	for p := range int32(partitions) {
		end, _ := e.log.End(e.ctx, p)
		if err := e.log.Read(e.ctx, p, 0, end, func(entry journal.Entry) error {
			batches[p] = append(batches[p], entry)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.model.Apply(e.ctx, batches); err != nil {
		t.Fatal(err)
	}
}

func (e *env) rating(t *testing.T, game engine.Key, user uuid.UUID) session.Rating {
	t.Helper()
	ratings, err := e.store.Ratings(e.ctx, game, []uuid.UUID{user})
	if err != nil {
		t.Fatal(err)
	}
	return ratings[user]
}

func (e *env) match(t *testing.T, node *owner.Owner, game engine.Key, a, b uuid.UUID) session.Session {
	t.Helper()
	for _, user := range []uuid.UUID{a, b} {
		if _, err := e.store.FindMatch(e.ctx, user, uuid.New(), game); err != nil {
			t.Fatal(err)
		}
	}
	paired, err := e.store.MatchRound(e.ctx)
	if err != nil || len(paired) != 1 {
		t.Fatal("pairings", paired, err)
	}
	ticket, err := e.store.Ticket(e.ctx, a, game)
	if err != nil || ticket.Session != nil {
		t.Fatal("unstarted match is visible", ticket, err)
	}
	pending, err := e.store.Pending(e.ctx)
	if err != nil || len(pending) != 1 || pending[0] != paired[0] {
		t.Fatal("pending", pending, err)
	}
	item := pending[0]
	started, err := node.Start(e.ctx, item.ID, item.Game, item.Creator, item.Opponent, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.Started(e.ctx, item); err != nil {
		t.Fatal(err)
	}
	if again, err := e.store.Pending(e.ctx); err != nil || len(again) != 0 {
		t.Fatal("started match still pending", again, err)
	}
	return started
}

func cell(i int) json.RawMessage {
	raw, _ := json.Marshal(tictactoe.Cell{Row: i / tictactoe.Size, Col: i % tictactoe.Size})
	return raw
}

func uci(move string) json.RawMessage {
	raw, _ := json.Marshal(chess.MoveData{UCI: move})
	return raw
}

func TestMatchedGameProjectsAndSettlesOnce(t *testing.T) {
	e := setup(t)
	node := e.node(t, "a")
	a, b := uuid.New(), uuid.New()
	game := e.match(t, node, tictactoe.Key, a, b)
	if !game.Rated || game.Opponent == nil || game.Clock == nil || game.State.Status != engine.StatusInProgress {
		t.Fatal(game)
	}
	for _, user := range []uuid.UUID{a, b} {
		ticket, err := e.store.Ticket(e.ctx, user, tictactoe.Key)
		if err != nil || ticket.Session == nil || *ticket.Session != game.ID {
			t.Fatal("ticket", ticket, err)
		}
	}
	if _, err := node.Undo(e.ctx, game.ID, a, nil); !errors.Is(err, session.ErrRated) {
		t.Fatal("undo allowed in rated game", err)
	}
	x, o := game.Creator, *game.Opponent
	var err error
	for ply, at := range []int{0, 3, 1, 4, 2} {
		player := x
		if ply%2 == 1 {
			player = o
		}
		game, err = node.Move(e.ctx, game.ID, player, game.State.Ply, tictactoe.Key, cell(at), &game.Revision)
		if err != nil {
			t.Fatal(err)
		}
	}
	if game.State.Status != engine.StatusWin {
		t.Fatal(game.State)
	}
	e.project(t)
	if got := e.rating(t, tictactoe.Key, x); got.Rating != 1220 || got.Wins != 1 || got.Games != 1 {
		t.Fatal("winner", got)
	}
	if got := e.rating(t, tictactoe.Key, o); got.Rating != 1180 || got.Losses != 1 {
		t.Fatal("loser", got)
	}
	if _, _, err := node.Leave(e.ctx, game.ID, o); err != nil {
		t.Fatal(err)
	}
	e.project(t)
	e.project(t)
	changes, err := e.store.Changes(e.ctx, []uuid.UUID{game.ID})
	if err != nil || len(changes[game.ID]) != 2 {
		t.Fatal("changes", changes, err)
	}
	if got := e.rating(t, tictactoe.Key, x); got.Games != 1 {
		t.Fatal("settled twice", got)
	}
	var moves int
	if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM game.moves WHERE session_id=$1`, game.ID).Scan(&moves); err != nil || moves != 5 {
		t.Fatal("moves", moves, err)
	}
	if _, err := e.store.Ticket(e.ctx, a, tictactoe.Key); !errors.Is(err, session.ErrNotSearching) {
		t.Fatal("finished ticket still visible", err)
	}
	ticket, err := e.store.FindMatch(e.ctx, a, uuid.New(), tictactoe.Key)
	if err != nil || ticket.Session != nil {
		t.Fatal("requeue after finished game", ticket, err)
	}
}

func TestRatedTimeoutAndResignSettle(t *testing.T) {
	e := setup(t)
	e.skew.Store(int64(-6 * time.Minute))
	node := e.node(t, "a")
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	timed := e.match(t, node, tictactoe.Key, a, b)
	e.skew.Store(0)
	read, err := node.Get(e.ctx, timed.ID, a)
	if err != nil || !read.TimedOut {
		t.Fatal("clock did not expire", read, err)
	}
	resigned := e.match(t, node, tictactoe.Key, c, d)
	loser := *resigned.Opponent
	game, cancelled, err := node.Leave(e.ctx, resigned.ID, loser)
	if err != nil || cancelled || !game.Resigned || game.State.Status != engine.StatusWin {
		t.Fatal(game, cancelled, err)
	}
	if _, _, err := node.Leave(e.ctx, resigned.ID, loser); err != nil {
		t.Fatal(err)
	}
	e.project(t)
	if got := e.rating(t, tictactoe.Key, timed.Creator); got.Rating != 1180 || got.Games != 1 {
		t.Fatal("timed out player", got)
	}
	if got := e.rating(t, tictactoe.Key, *timed.Opponent); got.Rating != 1220 {
		t.Fatal("waiting player", got)
	}
	if got := e.rating(t, tictactoe.Key, loser); got.Rating != 1180 || got.Losses != 1 || got.Games != 1 {
		t.Fatal("resigned player", got)
	}
}

func TestChessDrawAndCheckmateSettle(t *testing.T) {
	e := setup(t)
	node := e.node(t, "a")
	white, black := uuid.New(), uuid.New()
	game := e.match(t, node, chess.Key, white, black)
	if game.Clock.RemainingMS[0] != e.store.TimeLimitMS(chess.Key) || e.store.TimeLimitMS(chess.Key) != 600_000 {
		t.Fatal("chess clock", game.Clock)
	}
	first, second := game.Creator, *game.Opponent
	if _, err := node.Move(e.ctx, game.ID, first, 0, tictactoe.Key, uci("e2e4"), nil); !errors.Is(err, engine.ErrGameMismatch) {
		t.Fatal("wrong game accepted", err)
	}
	if _, err := node.Move(e.ctx, game.ID, first, 0, chess.Key, uci("e2e5"), nil); !errors.Is(err, chess.ErrIllegalMove) {
		t.Fatal("illegal move accepted", err)
	}
	if _, err := node.Draw(e.ctx, game.ID, second, session.AcceptDraw); !errors.Is(err, session.ErrNoDrawOffer) {
		t.Fatal("accepted a missing offer", err)
	}
	game, err := node.Draw(e.ctx, game.ID, first, session.OfferDraw)
	if err != nil || game.DrawOffer == nil || *game.DrawOffer != first {
		t.Fatal("offer", err)
	}
	if _, err := node.Draw(e.ctx, game.ID, first, session.AcceptDraw); !errors.Is(err, session.ErrNoDrawOffer) {
		t.Fatal("accepted own offer", err)
	}
	game, err = node.Move(e.ctx, game.ID, first, 0, chess.Key, uci("e2e4"), nil)
	if err != nil || game.DrawOffer != nil {
		t.Fatal("move kept the offer", err)
	}
	if _, err := node.Draw(e.ctx, game.ID, second, session.OfferDraw); err != nil {
		t.Fatal(err)
	}
	game, err = node.Draw(e.ctx, game.ID, first, session.DeclineDraw)
	if err != nil || game.DrawOffer != nil || game.State.Over() {
		t.Fatal("decline", err)
	}
	if _, err := node.Draw(e.ctx, game.ID, second, session.OfferDraw); err != nil {
		t.Fatal(err)
	}
	game, err = node.Draw(e.ctx, game.ID, first, session.AcceptDraw)
	if err != nil || !game.AgreedDraw || game.State.Status != engine.StatusDraw {
		t.Fatal("accept", err)
	}
	if _, err := node.Draw(e.ctx, game.ID, second, session.OfferDraw); !errors.Is(err, session.ErrNotPlaying) {
		t.Fatal("offer after the end", err)
	}
	mate := e.match(t, node, chess.Key, uuid.New(), uuid.New())
	players := []uuid.UUID{mate.Creator, *mate.Opponent}
	for ply, move := range []string{"f2f3", "e7e5", "g2g4", "d8h4"} {
		mate, err = node.Move(e.ctx, mate.ID, players[ply%2], ply, chess.Key, uci(move), &mate.Revision)
		if err != nil {
			t.Fatal(move, err)
		}
	}
	e.project(t)
	if got := e.rating(t, chess.Key, white); got.Rating != 1200 || got.Draws != 1 {
		t.Fatal("draw rating", got)
	}
	ratings, err := e.store.Ratings(e.ctx, chess.Key, players)
	if err != nil || ratings[players[1]].Rating != 1220 || ratings[players[0]].Rating != 1180 {
		t.Fatal("mate rating", ratings, err)
	}
}

func TestCancelledWaitingGameIsRemoved(t *testing.T) {
	e := setup(t)
	node := e.node(t, "a")
	creator, outsider := uuid.New(), uuid.New()
	game, err := node.Create(e.ctx, creator, uuid.New(), tictactoe.Key)
	if err != nil {
		t.Fatal(err)
	}
	e.project(t)
	listed, err := e.store.List(e.ctx, creator)
	if err != nil || len(listed) != 1 || listed[0].ID != game.ID || listed[0].Opponent != nil {
		t.Fatal("waiting game not projected", listed, err)
	}
	if _, _, err := node.Leave(e.ctx, game.ID, outsider); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("outsider left", err)
	}
	if _, cancelled, err := node.Leave(e.ctx, game.ID, creator); err != nil || !cancelled {
		t.Fatal(cancelled, err)
	}
	e.project(t)
	if listed, err := e.store.List(e.ctx, creator); err != nil || len(listed) != 0 {
		t.Fatal("cancelled game survived", listed, err)
	}
}

func TestProjectorFencesZombieOwner(t *testing.T) {
	e := setup(t)
	zombie := e.node(t, "a")
	x, o := uuid.New(), uuid.New()
	game, err := zombie.Start(e.ctx, session.SessionID(x, uuid.New()), tictactoe.Key, x, o, false)
	if err != nil {
		t.Fatal(err)
	}
	game, err = zombie.Move(e.ctx, game.ID, x, 0, tictactoe.Key, cell(0), &game.Revision)
	if err != nil {
		t.Fatal(err)
	}
	successor := e.node(t, "b")
	if _, err := zombie.Move(e.ctx, game.ID, o, 1, tictactoe.Key, cell(8), &game.Revision); err != nil {
		t.Fatal(err)
	}
	next, err := successor.Move(e.ctx, game.ID, o, 1, tictactoe.Key, cell(4), &game.Revision)
	if err != nil {
		t.Fatal(err)
	}
	e.project(t)
	stored, err := e.model.Load(e.ctx, game.ID)
	if err != nil || stored.Revision != next.Revision {
		t.Fatal("read model revision", stored.Revision, next.Revision, err)
	}
	have, _, err := tictactoe.Position(stored.State)
	if err != nil {
		t.Fatal(err)
	}
	want, _, _ := tictactoe.Position(next.State)
	if have != want || have[8] >= 0 {
		t.Fatal("zombie write reached the read model", have, want)
	}
	owners, err := e.model.Owners(e.ctx)
	if err != nil || owners[journal.PartitionOf(game.ID, partitions)] != "b:8080" {
		t.Fatal("routing table", owners, err)
	}
	projected, err := e.model.Projected(e.ctx, journal.PartitionOf(game.ID, partitions))
	if err != nil || projected.Generation != 2 {
		t.Fatal("claim generation", projected, err)
	}
}

func TestProjectionReplaysCompactMovesFromAnyOffset(t *testing.T) {
	e := setup(t)
	node := e.node(t, "a")
	game := e.match(t, node, chess.Key, uuid.New(), uuid.New())
	players := []uuid.UUID{game.Creator, *game.Opponent}
	opening := []string{"e2e4", "e7e5", "g1f3", "b8c6", "f1b5", "a7a6", "b5a4", "g8f6", "e1g1", "f8e7"}
	var err error
	for ply, move := range opening[:4] {
		if game, err = node.Move(e.ctx, game.ID, players[ply%2], ply, chess.Key, uci(move), &game.Revision); err != nil {
			t.Fatal(move, err)
		}
	}
	e.project(t)
	for ply, move := range opening[4:] {
		if game, err = node.Move(e.ctx, game.ID, players[(ply+4)%2], ply+4, chess.Key, uci(move), &game.Revision); err != nil {
			t.Fatal(move, err)
		}
	}
	e.project(t)
	check := func(stage string) {
		t.Helper()
		projected, err := e.model.Load(e.ctx, game.ID)
		var want, got any
		_ = json.Unmarshal(game.State.Data, &want)
		_ = json.Unmarshal(projected.State.Data, &got)
		if err != nil || projected.Revision != game.Revision || !reflect.DeepEqual(want, got) {
			t.Fatal(stage, "projected state differs", projected.Revision, game.Revision, err)
		}
		var moves, snapshots int
		if err := e.pool.QueryRow(e.ctx, `SELECT count(*), count(state_before) FROM game.moves WHERE session_id=$1`, game.ID).Scan(&moves, &snapshots); err != nil {
			t.Fatal(err)
		}
		if moves != len(opening) || snapshots != 0 {
			t.Fatal(stage, "moves", moves, "snapshots", snapshots)
		}
	}
	check("incremental")
	if _, err := e.pool.Exec(e.ctx, `UPDATE game.projector_offsets SET next_offset=0`); err != nil {
		t.Fatal(err)
	}
	e.model.Forget()
	e.project(t)
	check("replayed from zero")
}
