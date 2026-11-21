package projection_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/projection"
	"github.com/bvivg/axon/core/services/game/internal/session"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
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
	for _, table := range []string{"game.outbox", "game.match_queue", "game.rating_changes", "game.moves", "game.sessions", "game.ratings", "game.owners", "game.projector_offsets"} {
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
	model := projection.New(store, session.NewRules(games))
	if _, err := model.EnsureMovePartitions(ctx, 2); err != nil {
		t.Fatal(err)
	}
	return &env{ctx: ctx, pool: pool, store: store, model: model, log: journal.NewMemory(partitions), rules: session.NewRules(games)}
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
		{"000007_moves_partitioned", `SELECT EXISTS (SELECT 1 FROM pg_partitioned_table WHERE partrelid = 'game.moves'::regclass)`},
		{"000008_outbox", `SELECT to_regclass('game.outbox') IS NOT NULL`},
		{"000009_live_sessions", `SELECT to_regclass('game.game_sessions_live') IS NOT NULL`},
		{"000010_tournaments", `SELECT to_regclass('game.cancelled_tournaments') IS NOT NULL`},
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

func (e *env) finished(t *testing.T, id uuid.UUID) []*eventsv1.GameFinished {
	t.Helper()
	rows, err := e.pool.Query(e.ctx, `SELECT id, aggregateid, type, payload FROM game.outbox WHERE aggregatetype = $1 ORDER BY created_at`, projection.FinishedTopic)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []*eventsv1.GameFinished
	for rows.Next() {
		var eventID uuid.UUID
		var key, kind string
		var payload []byte
		if err := rows.Scan(&eventID, &key, &kind, &payload); err != nil {
			t.Fatal(err)
		}
		event := &eventsv1.GameFinished{}
		if err := proto.Unmarshal(payload, event); err != nil {
			t.Fatal(err)
		}
		if event.SessionId != id.String() {
			continue
		}
		if eventID != projection.FinishedEventID(id) || event.EventId != eventID.String() || key != event.Game || kind != "axon.events.v1.GameFinished" {
			t.Fatal("outbox envelope", eventID, key, kind, event)
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *env) finishedOnce(t *testing.T, game session.Session, outcome eventsv1.GameFinished_Outcome, winner int32) *eventsv1.GameFinished {
	t.Helper()
	events := e.finished(t, game.ID)
	if len(events) != 1 {
		t.Fatal("finished events for", game.ID, len(events))
	}
	event := events[0]
	players := []string{game.Creator.String(), game.Opponent.String()}
	if event.Outcome != outcome || event.WinnerSeat != winner || event.Rated != game.Rated || event.Game != string(game.Game) ||
		!reflect.DeepEqual(event.Players, players) || !event.FinishedAt.IsValid() {
		t.Fatal("finished event", event)
	}
	return event
}

func (e *env) match(t *testing.T, node *owner.Owner, game engine.Key, a, b uuid.UUID) session.Session {
	t.Helper()
	for _, user := range []uuid.UUID{a, b} {
		if _, err := e.store.FindMatch(e.ctx, user, uuid.New(), game, session.Table{}); err != nil {
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
	if err != nil || len(pending) != 1 || pending[0].ID != paired[0].ID || !slices.Equal(pending[0].Players, paired[0].Players) {
		t.Fatal("pending", pending, err)
	}
	item := pending[0]
	started, err := node.Start(e.ctx, item.ID, item.Game, item.Players, item.Table, true)
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
	event := e.finishedOnce(t, game, eventsv1.GameFinished_OUTCOME_WIN, 0)
	if !event.FinishedAt.AsTime().Equal(game.UpdatedAt) {
		t.Fatal("finished_at is not the journal time", event.FinishedAt.AsTime(), game.UpdatedAt)
	}
	if got := e.rating(t, tictactoe.Key, x); got.Rating != session.InitialRating || got.Games != 0 {
		t.Fatal("the projector settled a rating itself", got)
	}
	if _, _, err := node.Leave(e.ctx, game.ID, o); err != nil {
		t.Fatal(err)
	}
	e.project(t)
	e.project(t)
	e.finishedOnce(t, game, eventsv1.GameFinished_OUTCOME_WIN, 0)
	update := session.RatingUpdate{Session: game.ID, Game: tictactoe.Key, At: game.UpdatedAt, Players: []session.RatedPlayer{
		{User: x, Before: 1200, After: 1362, Games: 1, Wins: 1},
		{User: o, Before: 1200, After: 1038, Games: 1, Losses: 1},
	}}
	for range 2 {
		if err := e.store.ApplyRatings(e.ctx, update); err != nil {
			t.Fatal(err)
		}
	}
	changes, err := e.store.Changes(e.ctx, []uuid.UUID{game.ID})
	if err != nil || len(changes[game.ID]) != 2 {
		t.Fatal("changes", changes, err)
	}
	for _, change := range changes[game.ID] {
		if !change.At.Equal(game.UpdatedAt) || change.Before != 1200 || (change.User == x) != (change.After == 1362) {
			t.Fatal("change", change)
		}
	}
	if got := e.rating(t, tictactoe.Key, x); got.Rating != 1362 || got.Games != 1 || got.Wins != 1 {
		t.Fatal("winner", got)
	}
	if got := e.rating(t, tictactoe.Key, o); got.Rating != 1038 || got.Games != 1 || got.Losses != 1 {
		t.Fatal("loser", got)
	}
	var moves int
	if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM game.moves WHERE session_id=$1`, game.ID).Scan(&moves); err != nil || moves != 5 {
		t.Fatal("moves", moves, err)
	}
	if _, err := e.store.Ticket(e.ctx, a, tictactoe.Key); !errors.Is(err, session.ErrNotSearching) {
		t.Fatal("finished ticket still visible", err)
	}
	ticket, err := e.store.FindMatch(e.ctx, a, uuid.New(), tictactoe.Key, session.Table{})
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
	e.finishedOnce(t, timed, eventsv1.GameFinished_OUTCOME_WIN, 1)
	e.finishedOnce(t, resigned, eventsv1.GameFinished_OUTCOME_WIN, 0)
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
	e.finishedOnce(t, game, eventsv1.GameFinished_OUTCOME_DRAW, -1)
	e.finishedOnce(t, mate, eventsv1.GameFinished_OUTCOME_WIN, 1)
	requests := e.analysisRequests(t)
	if want := []string{"f2f3", "e7e5", "g2g4", "d8h4"}; len(requests[mate.ID]) != 1 || !slices.Equal(requests[mate.ID][0].GetMoves(), want) {
		t.Fatal("analysis requests for the mate", requests[mate.ID])
	}
	if want := []string{"e2e4"}; len(requests[game.ID]) != 1 || !slices.Equal(requests[game.ID][0].GetMoves(), want) {
		t.Fatal("analysis requests for the agreed draw", requests[game.ID])
	}
}

func (e *env) analysisRequests(t *testing.T) map[uuid.UUID][]*eventsv1.GameFinished {
	t.Helper()
	rows, err := e.pool.Query(e.ctx, `SELECT id, aggregateid, payload FROM game.outbox WHERE aggregatetype = $1`, projection.AnalysisTopic)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[uuid.UUID][]*eventsv1.GameFinished{}
	for rows.Next() {
		var eventID uuid.UUID
		var key string
		var payload []byte
		if err := rows.Scan(&eventID, &key, &payload); err != nil {
			t.Fatal(err)
		}
		event := &eventsv1.GameFinished{}
		if err := proto.Unmarshal(payload, event); err != nil {
			t.Fatal(err)
		}
		id := uuid.MustParse(event.SessionId)
		if key != id.String() || eventID != projection.AnalysisEventID(id) || !event.Rated {
			t.Fatal("analysis request envelope", eventID, key, event)
		}
		out[id] = append(out[id], event)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFinishedRatedSessionWritesOneOutboxRowAcrossReplays(t *testing.T) {
	e := setup(t)
	node := e.node(t, "a")
	game := e.match(t, node, tictactoe.Key, uuid.New(), uuid.New())
	players := []uuid.UUID{game.Creator, *game.Opponent}
	var err error
	for ply, at := range []int{0, 3, 1, 4, 2} {
		if game, err = node.Move(e.ctx, game.ID, players[ply%2], ply, tictactoe.Key, cell(at), &game.Revision); err != nil {
			t.Fatal(err)
		}
	}
	unrated, err := node.Start(e.ctx, session.SessionID(players[0], uuid.New()), tictactoe.Key, []uuid.UUID{players[0], players[1]}, session.Table{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if unrated, _, err = node.Leave(e.ctx, unrated.ID, players[1]); err != nil {
		t.Fatal(err)
	}
	e.project(t)
	count := func() int {
		var rows int
		if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM game.outbox WHERE aggregatetype = $1`, projection.FinishedTopic).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if got := count(); got != 2 {
		t.Fatal("outbox rows after the first projection", got)
	}
	e.finishedOnce(t, game, eventsv1.GameFinished_OUTCOME_WIN, 0)
	e.finishedOnce(t, unrated, eventsv1.GameFinished_OUTCOME_WIN, 0)
	if requests := e.analysisRequests(t); len(requests) != 0 {
		t.Fatal("tic-tac-toe games requested an analysis", requests)
	}
	if _, err := e.pool.Exec(e.ctx, `UPDATE game.projector_offsets SET next_offset=0`); err != nil {
		t.Fatal(err)
	}
	e.model.Forget()
	e.project(t)
	if got := count(); got != 2 {
		t.Fatal("a replay from offset zero added outbox rows", got)
	}
	for _, statement := range []string{`DELETE FROM game.match_queue`, `DELETE FROM game.moves`, `DELETE FROM game.sessions`, `UPDATE game.projector_offsets SET next_offset=0`} {
		if _, err := e.pool.Exec(e.ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	e.model.Forget()
	e.project(t)
	if got := count(); got != 2 {
		t.Fatal("rebuilding the read model from the journal added outbox rows", got)
	}
	e.finishedOnce(t, game, eventsv1.GameFinished_OUTCOME_WIN, 0)
}

func TestCancelledWaitingGameIsRemoved(t *testing.T) {
	e := setup(t)
	node := e.node(t, "a")
	creator, outsider := uuid.New(), uuid.New()
	game, err := node.Create(e.ctx, creator, uuid.New(), tictactoe.Key, session.Table{})
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
	game, err := zombie.Start(e.ctx, session.SessionID(x, uuid.New()), tictactoe.Key, []uuid.UUID{x, o}, session.Table{}, false)
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

type moveRow struct {
	Ply       int
	Partition string
	PlayedAt  time.Time
	Player    uuid.UUID
	Data      string
	Clock     string
}

func (e *env) moveRows(t *testing.T, id uuid.UUID) []moveRow {
	t.Helper()
	rows, err := e.pool.Query(e.ctx, `SELECT m.ply, c.relname, m.played_at, m.player_id, m.data::text, COALESCE(m.clock_before::text, '')
		FROM game.moves m JOIN pg_class c ON c.oid = m.tableoid WHERE m.session_id=$1 ORDER BY m.ply, m.played_at`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []moveRow
	for rows.Next() {
		var row moveRow
		if err := rows.Scan(&row.Ply, &row.Partition, &row.PlayedAt, &row.Player, &row.Data, &row.Clock); err != nil {
			t.Fatal(err)
		}
		row.PlayedAt = row.PlayedAt.UTC()
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func monthPartition(at time.Time) string {
	return at.UTC().Format("moves_2006_01")
}

func TestMovesAcrossMonthsLandInMonthlyPartitionsAndReplayIdentically(t *testing.T) {
	e := setup(t)
	node := e.node(t, "a")
	x, o := uuid.New(), uuid.New()
	game, err := node.Start(e.ctx, session.SessionID(x, uuid.New()), tictactoe.Key, []uuid.UUID{x, o}, session.Table{}, false)
	if err != nil {
		t.Fatal(err)
	}
	for ply, at := range []int{0, 4} {
		player := []uuid.UUID{x, o}[ply%2]
		if game, err = node.Move(e.ctx, game.ID, player, ply, tictactoe.Key, cell(at), &game.Revision); err != nil {
			t.Fatal(err)
		}
	}
	e.project(t)
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month()+1, 2, 12, 0, 0, 0, time.UTC)
	e.skew.Store(int64(time.Until(next)))
	if game, err = node.AdjustClock(e.ctx, game.ID, []int64{300_000, 300_000}); err != nil {
		t.Fatal(err)
	}
	if game, err = node.Undo(e.ctx, game.ID, o, &game.Revision); err != nil || game.State.Ply != 1 {
		t.Fatal("undo", game.State.Ply, err)
	}
	if game, err = node.Move(e.ctx, game.ID, o, 1, tictactoe.Key, cell(8), &game.Revision); err != nil {
		t.Fatal(err)
	}
	if game, err = node.Move(e.ctx, game.ID, x, 2, tictactoe.Key, cell(1), &game.Revision); err != nil {
		t.Fatal(err)
	}
	e.project(t)
	want := e.moveRows(t, game.ID)
	if len(want) != 3 || want[0].Ply != 1 || want[1].Ply != 2 || want[2].Ply != 3 {
		t.Fatal("redone ply must replace the undone row", want)
	}
	var redone tictactoe.Cell
	if err := json.Unmarshal([]byte(want[1].Data), &redone); err != nil || redone != (tictactoe.Cell{Row: 2, Col: 2}) {
		t.Fatal("ply 2 kept the undone move", want[1].Data, err)
	}
	first, second := monthPartition(want[0].PlayedAt), monthPartition(next)
	if first == second || want[0].Partition != first || want[1].Partition != second || want[2].Partition != second {
		t.Fatal("moves are not split by month", first, second, want)
	}
	if !want[2].PlayedAt.Equal(game.UpdatedAt.UTC().Truncate(time.Microsecond)) {
		t.Fatal("played_at must be the journal time of the move", want[2].PlayedAt, game.UpdatedAt)
	}
	if _, err := e.pool.Exec(e.ctx, `UPDATE game.projector_offsets SET next_offset=0`); err != nil {
		t.Fatal(err)
	}
	e.model.Forget()
	e.project(t)
	if got := e.moveRows(t, game.ID); !reflect.DeepEqual(got, want) {
		t.Fatal("replay over the read model changed moves", got, want)
	}
	for _, statement := range []string{`DELETE FROM game.moves WHERE session_id=$1`, `DELETE FROM game.sessions WHERE id=$1`} {
		if _, err := e.pool.Exec(e.ctx, statement, game.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.pool.Exec(e.ctx, `UPDATE game.projector_offsets SET next_offset=0, claim_generation=0`); err != nil {
		t.Fatal(err)
	}
	e.model.Forget()
	e.project(t)
	if got := e.moveRows(t, game.ID); !reflect.DeepEqual(got, want) {
		t.Fatal("rebuild from the journal produced different moves", got, want)
	}
}

func TestEnsureMovePartitionsAdoptsRowsFromDefault(t *testing.T) {
	e := setup(t)
	node := e.node(t, "a")
	x, o := uuid.New(), uuid.New()
	game, err := node.Start(e.ctx, session.SessionID(x, uuid.New()), tictactoe.Key, []uuid.UUID{x, o}, session.Table{}, false)
	if err != nil {
		t.Fatal(err)
	}
	e.project(t)
	now := time.Now().UTC()
	later := time.Date(now.Year(), now.Month()+9, 15, 0, 0, 0, 0, time.UTC)
	partition := monthPartition(later)
	drop := func() {
		if _, err := e.pool.Exec(e.ctx, `DROP TABLE IF EXISTS game.`+partition); err != nil {
			t.Fatal(err)
		}
	}
	drop()
	t.Cleanup(func() {
		_, _ = e.pool.Exec(e.ctx, `DELETE FROM game.moves WHERE session_id=$1`, game.ID)
		drop()
	})
	if _, err := e.pool.Exec(e.ctx, `INSERT INTO game.moves (session_id, ply, player_id, data, played_at) VALUES ($1, 1, $2, '{}', $3)`, game.ID, x, later); err != nil {
		t.Fatal(err)
	}
	if rows := e.moveRows(t, game.ID); len(rows) != 1 || rows[0].Partition != "moves_default" {
		t.Fatal("row outside known months must land in the default partition", rows)
	}
	created, err := e.model.EnsureMovePartitions(e.ctx, 9)
	if err != nil || created < 1 {
		t.Fatal("ensure", created, err)
	}
	if rows := e.moveRows(t, game.ID); len(rows) != 1 || rows[0].Partition != partition {
		t.Fatal("default row was not moved into its month", rows)
	}
	if again, err := e.model.EnsureMovePartitions(e.ctx, 9); err != nil || again != 0 {
		t.Fatal("ensure is not idempotent", again, err)
	}
}
