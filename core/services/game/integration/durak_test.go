//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/bvivg/axon/core/services/game/internal/cluster"
	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/durak"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/projection"
	"github.com/bvivg/axon/core/services/game/internal/route"
	"github.com/bvivg/axon/core/services/game/internal/server"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
)

type durakNode struct {
	owner  *owner.Owner
	client gamev1connect.GameServiceClient
	stop   func()
}

func startDurakNode(t *testing.T, id string, cfg journal.KafkaConfig, store *session.Store, model *projection.Postgres, rules session.Rules) *durakNode {
	t.Helper()
	log, err := journal.NewKafka(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	own := owner.New(owner.Config{ID: id, Address: id + ":8083", Log: log, Model: model, Rules: rules, Now: time.Now})
	handler := server.New(server.Config{Store: store, Owner: own, Routes: route.New(model.Owners), Self: id + ":8083", TestHooks: true})
	own.Listen(handler)
	ownership := spawn(func(ctx context.Context) {
		defer log.Close()
		_ = cluster.NewOwnership(cfg, own, quiet).Run(ctx)
	})
	mux := http.NewServeMux()
	mux.Handle(gamev1connect.NewGameServiceHandler(handler))
	srv := httptest.NewServer(mux)
	node := &durakNode{owner: own, client: gamev1connect.NewGameServiceClient(srv.Client(), srv.URL)}
	stopped := false
	node.stop = func() {
		if stopped {
			return
		}
		stopped = true
		srv.Close()
		ownership.stop()
	}
	t.Cleanup(node.stop)
	eventually(t, id+" to own every partition", func() bool { return len(own.Owned()) == partitions })
	return node
}

func sameData(a, b engine.State) bool {
	var left, right any
	return json.Unmarshal(a.Data, &left) == nil && json.Unmarshal(b.Data, &right) == nil && reflect.DeepEqual(left, right)
}

func caller[T any](user uuid.UUID, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set(server.TestUserHeader, user.String())
	return req
}

type durakCards struct {
	Hands [2][]string `json:"hands"`
	Stock []string    `json:"stock"`
	Trump string      `json:"trump"`
	Moves []string    `json:"moves"`
}

func unseen(t *testing.T, state engine.State, seat int) []string {
	t.Helper()
	var d durakCards
	if err := json.Unmarshal(state.Data, &d); err != nil {
		t.Fatal(err)
	}
	public := map[string]bool{d.Trump: true}
	for _, m := range d.Moves {
		for _, word := range strings.Fields(m)[1:] {
			public[word] = true
		}
	}
	var out []string
	for s, hand := range d.Hands {
		if s != seat {
			for _, c := range hand {
				if !public[c] {
					out = append(out, c)
				}
			}
		}
	}
	for _, c := range d.Stock {
		if !public[c] {
			out = append(out, c)
		}
	}
	return out
}

func noLeak(t *testing.T, where string, raw []byte, secrets []string) {
	t.Helper()
	for _, c := range secrets {
		if regexp.MustCompile(`(^|[^A-Za-z0-9_])` + c + `([^A-Za-z0-9_]|$)`).Match(raw) {
			t.Fatalf("%s leaked %s: %s", where, c, raw)
		}
	}
	if strings.Contains(string(raw), "seed") {
		t.Fatalf("%s leaked the seed: %s", where, raw)
	}
}

func checkBoard(t *testing.T, where string, board *gamev1.DurakBoard, seat int) {
	t.Helper()
	if len(board.GetHands()) != 2 {
		t.Fatalf("%s: hands %v", where, board.GetHands())
	}
	for s, hand := range board.GetHands() {
		if s == seat && (hand.GetHidden() || len(hand.GetCards()) != int(hand.GetCount())) {
			t.Fatalf("%s: seat %d cannot see its own hand: %+v", where, seat, hand)
		}
		if s != seat && (!hand.GetHidden() || len(hand.GetCards()) != 0) {
			t.Fatalf("%s: seat %d sees seat %d: %+v", where, seat, s, hand)
		}
	}
}

func TestDurakDealSurvivesTheJournalReadModelAndFailoverWithoutLeaking(t *testing.T) {
	ctx := context.Background()
	for _, table := range []string{"outbox", "match_queue", "rating_changes", "moves", "sessions", "ratings", "owners", "projector_offsets"} {
		if _, err := pool.Exec(ctx, `DELETE FROM game.`+table); err != nil {
			t.Fatal(err)
		}
	}
	games := engine.NewRegistry()
	if err := games.Register(durak.Definition()); err != nil {
		t.Fatal(err)
	}
	store := session.NewStore(pool, games)
	rules := session.NewRules(games)
	model := projection.New(store, rules)
	if _, err := model.EnsureMovePartitions(ctx, 2); err != nil {
		t.Fatal(err)
	}
	suffix := uuid.NewString()[:8]
	cfg := journal.KafkaConfig{Brokers: brokers, Topic: "game.session-events.d-" + suffix, LeaseTopic: "game.partition-leases.d-" + suffix, Partitions: partitions, ReplicationFactor: 1}
	projector := spawn(func(ctx context.Context) { _ = cluster.NewProjector(cfg, model, quiet, nil).Run(ctx) })
	t.Cleanup(projector.stop)
	first := startDurakNode(t, "first", cfg, store, model, rules)

	a, b, stranger := uuid.New(), uuid.New(), uuid.New()
	created, err := first.client.CreateSession(ctx, caller(a, &gamev1.CreateSessionRequest{Game: string(durak.Key), RequestId: uuid.NewString()}))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse(created.Msg.GetSession().GetId())
	if _, err := first.client.JoinSession(ctx, caller(b, &gamev1.JoinSessionRequest{SessionId: id.String()})); err != nil {
		t.Fatal(err)
	}
	users := []uuid.UUID{a, b}
	play := func(node *durakNode, plies int) session.Session {
		t.Helper()
		game, err := node.owner.Get(ctx, id, a)
		if err != nil {
			t.Fatal(err)
		}
		for range plies {
			if game.State.Over() {
				break
			}
			seat := game.State.Turn
			legal := durak.New().ValidMoves(game.State, game.State.Players[seat])
			if game, err = node.owner.Move(ctx, id, users[seat], game.State.Ply, durak.Key, legal[(game.State.Ply*5)%len(legal)].Data, &game.Revision); err != nil {
				t.Fatal(err)
			}
		}
		return game
	}
	projected := func(game session.Session) {
		t.Helper()
		eventually(t, "the read model to reach the owner", func() bool {
			stored, err := model.Load(ctx, id)
			return err == nil && stored.Revision == game.Revision
		})
		stored, err := model.Load(ctx, id)
		if err != nil || stored.State.Seed != game.State.Seed || stored.State.Seed == 0 || !sameData(stored.State, game.State) {
			t.Fatalf("the read model diverged from the owner: seed %d/%d, %v", stored.State.Seed, game.State.Seed, err)
		}
	}

	live := play(first, 40)
	if live.State.Over() {
		t.Fatal("the game ended before the live checks")
	}
	projected(live)
	for seat, viewer := range []uuid.UUID{a, b, stranger} {
		if seat == 2 {
			seat = engine.NoSeat
		}
		history, err := first.client.GetSessionHistory(ctx, caller(viewer, &gamev1.GetSessionHistoryRequest{SessionId: id.String()}))
		if err != nil {
			t.Fatal(err)
		}
		frames := history.Msg.GetFrames()
		if len(frames) != live.State.Ply+1 {
			t.Fatalf("history has %d frames for %d plies", len(frames), live.State.Ply)
		}
		raw, _ := protojson.Marshal(history.Msg)
		noLeak(t, "live history", raw, unseen(t, live.State, seat))
		checkBoard(t, "live history session", history.Msg.GetSession().GetDurak(), seat)
		for i, frame := range frames {
			checkBoard(t, fmt.Sprintf("live history frame %d", i), frame.GetDurak(), seat)
		}
	}
	key := string(durak.Key)
	listed, err := first.client.ListLiveSessions(ctx, caller(stranger, &gamev1.ListLiveSessionsRequest{Game: &key}))
	if err != nil || len(listed.Msg.GetSessions()) != 1 {
		t.Fatal("live list", listed, err)
	}
	raw, _ := protojson.Marshal(listed.Msg)
	noLeak(t, "live list", raw, unseen(t, live.State, engine.NoSeat))
	checkBoard(t, "live list", listed.Msg.GetSessions()[0].GetDurak(), engine.NoSeat)
	mine, err := first.client.ListSessions(ctx, caller(a, &gamev1.ListSessionsRequest{}))
	if err != nil || len(mine.Msg.GetSessions()) != 1 {
		t.Fatal("own list", mine, err)
	}
	raw, _ = protojson.Marshal(mine.Msg)
	noLeak(t, "own list", raw, unseen(t, live.State, 0))
	checkBoard(t, "own list", mine.Msg.GetSessions()[0].GetDurak(), 0)

	first.stop()
	second := startDurakNode(t, "second", cfg, store, model, rules)
	resumed, err := second.owner.Get(ctx, id, a)
	if err != nil || resumed.Revision != live.Revision || resumed.State.Seed != live.State.Seed || !sameData(resumed.State, live.State) {
		t.Fatalf("failover changed the game: %v", err)
	}
	final := play(second, durak.MaxPlies)
	if !final.State.Over() {
		t.Fatal("the game did not finish")
	}
	projected(final)
	history, err := second.client.GetSessionHistory(ctx, caller(stranger, &gamev1.GetSessionHistoryRequest{SessionId: id.String()}))
	if err != nil {
		t.Fatal(err)
	}
	frames := history.Msg.GetFrames()
	if len(frames) != final.State.Ply+1 || frames[len(frames)-1].GetDurak().GetEnd() == gamev1.DurakEnd_DURAK_END_UNSPECIFIED {
		t.Fatal("finished history", len(frames), final.State.Ply)
	}
	for _, hand := range frames[len(frames)/2].GetDurak().GetHands() {
		if hand.GetHidden() || len(hand.GetCards()) != int(hand.GetCount()) {
			t.Fatal("a finished replay hides a hand", hand)
		}
	}
	if raw, _ := protojson.Marshal(history.Msg); strings.Contains(string(raw), "seed") {
		t.Fatal("the seed left the service")
	}
	eventually(t, "the finished event", func() bool {
		var events int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM game.outbox WHERE aggregatetype = $1 AND aggregateid = $2`, projection.FinishedTopic, string(durak.Key)).Scan(&events); err != nil {
			t.Fatal(err)
		}
		return events == 1
	})
}
