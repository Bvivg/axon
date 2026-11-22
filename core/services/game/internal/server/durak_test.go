package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/durak"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

type emptyModel struct{}

func (emptyModel) Claim(context.Context, int32, string, string, time.Duration) (owner.Lease, error) {
	return owner.Lease{Generation: 1}, nil
}

func (emptyModel) Renew(_ context.Context, _ string, held map[int32]int64, _ time.Duration) ([]int32, error) {
	out := make([]int32, 0, len(held))
	for partition := range held {
		out = append(out, partition)
	}
	return out, nil
}

func (emptyModel) Release(context.Context, string, int32, int64) error { return nil }

func (emptyModel) Projected(context.Context, int32) (owner.Projection, error) {
	return owner.Projection{}, nil
}

func (emptyModel) Open(context.Context, int32, int32) ([]session.Session, error) { return nil, nil }

func (emptyModel) Load(context.Context, uuid.UUID) (session.Session, error) {
	return session.Session{}, session.ErrNotFound
}

func (emptyModel) MoveBefore(context.Context, uuid.UUID, int) (session.MoveRecord, bool, error) {
	return session.MoveRecord{}, false, nil
}

func (emptyModel) Moves(context.Context, uuid.UUID, int) ([]session.MoveRecord, error) {
	return nil, nil
}

func cardGames(t *testing.T) *engine.Registry {
	t.Helper()
	games := engine.NewRegistry()
	for _, def := range []engine.Definition{tictactoe.Definition(), durak.Definition()} {
		if err := games.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	return games
}

type stand struct {
	owner  *owner.Owner
	server *httptest.Server
	client gamev1connect.GameServiceClient
}

func newStand(t *testing.T) *stand {
	t.Helper()
	games := cardGames(t)
	own := owner.New(owner.Config{ID: "a", Address: "a", Log: journal.NewMemory(4), Model: emptyModel{}, Rules: session.NewRules(games)})
	for partition := range int32(4) {
		if err := own.Assign(context.Background(), partition); err != nil {
			t.Fatal(err)
		}
	}
	h := New(Config{Store: session.NewStore(nil, games), Owner: own, TestHooks: true})
	own.Listen(h)
	mux := http.NewServeMux()
	mux.Handle(gamev1connect.NewGameServiceHandler(h))
	mux.HandleFunc("GET /ws", h.Socket)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &stand{owner: own, server: srv, client: gamev1connect.NewGameServiceClient(srv.Client(), srv.URL)}
}

func asUser[T any](user uuid.UUID, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set(TestUserHeader, user.String())
	return req
}

type frame struct {
	raw     []byte
	session *gamev1.Session
	err     gamev1.SessionError
}

type watcher struct {
	t      *testing.T
	conn   *ws.Conn
	frames chan frame
}

func (s *stand) watch(t *testing.T, user uuid.UUID, id string) *watcher {
	t.Helper()
	return s.open(t, user, &gamev1.SessionCommand{Action: &gamev1.SessionCommand_Subscribe{Subscribe: id}})
}

func (s *stand) open(t *testing.T, user uuid.UUID, first *gamev1.SessionCommand) *watcher {
	t.Helper()
	header := http.Header{}
	header.Set(TestUserHeader, user.String())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(s.server.URL, "http")+"/ws", ws.DialOptions{Subprotocols: []string{Protocol}, Header: header})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	w := &watcher{t: t, conn: conn, frames: make(chan frame, 8192)}
	w.send(first)
	go func() {
		defer close(w.frames)
		for {
			raw, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			var event gamev1.SessionEvent
			if protojson.Unmarshal(raw, &event) != nil {
				return
			}
			w.frames <- frame{raw: raw, session: event.GetSession(), err: event.GetError()}
		}
	}()
	return w
}

func (w *watcher) send(command *gamev1.SessionCommand) {
	w.t.Helper()
	raw, err := protojson.Marshal(command)
	if err != nil {
		w.t.Fatal(err)
	}
	if err := w.conn.Write(w.t.Context(), raw); err != nil {
		w.t.Fatal(err)
	}
}

func (w *watcher) next() frame {
	w.t.Helper()
	select {
	case f, ok := <-w.frames:
		if !ok {
			w.t.Fatal("socket closed")
		}
		return f
	case <-time.After(10 * time.Second):
		w.t.Fatal("no frame")
		return frame{}
	}
}

type fullData struct {
	Hands [2][]string `json:"hands"`
	Stock []string    `json:"stock"`
	Trump string      `json:"trump"`
	Moves []string    `json:"moves"`
}

func secrets(t *testing.T, state engine.State, seat int) []string {
	t.Helper()
	var d fullData
	if err := json.Unmarshal(state.Data, &d); err != nil || d.Stock == nil && d.Hands[0] == nil {
		t.Fatalf("not a full state: %s %v", state.Data, err)
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
			out = append(out, hand...)
		}
	}
	out = append(out, d.Stock...)
	return slices.DeleteFunc(out, func(c string) bool { return public[c] })
}

func leaked(raw []byte, cards []string) []string {
	var out []string
	for _, c := range cards {
		if regexp.MustCompile(`(^|[^A-Za-z0-9_])` + c + `([^A-Za-z0-9_]|$)`).Match(raw) {
			out = append(out, c)
		}
	}
	return out
}

func checkHidden(t *testing.T, where string, raw []byte, out *gamev1.Session, full engine.State, seat int) {
	t.Helper()
	board := out.GetDurak()
	if out.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_PLAYING {
		for s, hand := range board.GetHands() {
			if hand.GetHidden() || len(hand.GetCards()) != int(hand.GetCount()) {
				t.Fatalf("%s: finished game hides seat %d", where, s)
			}
		}
		return
	}
	if len(board.GetHands()) != 2 {
		t.Fatalf("%s: hands %v", where, board.GetHands())
	}
	for s, hand := range board.GetHands() {
		mine := s == seat
		if mine && (hand.GetHidden() || len(hand.GetCards()) != int(hand.GetCount())) || !mine && (!hand.GetHidden() || len(hand.GetCards()) != 0) {
			t.Fatalf("%s: seat %d sees seat %d as %+v", where, seat, s, hand)
		}
	}
	if len(board.GetLegalMoves()) > 0 && seat != full.Turn {
		t.Fatalf("%s: seat %d got the legal moves of seat %d: %v", where, seat, full.Turn, board.GetLegalMoves())
	}
	if found := leaked(raw, secrets(t, full, seat)); len(found) > 0 {
		t.Fatalf("%s: seat %d received hidden cards %v in %s", where, seat, found, raw)
	}
	if strings.Contains(string(raw), "seed") || strings.Contains(string(raw), "stock\":[") {
		t.Fatalf("%s: seed or stock order in %s", where, raw)
	}
}

func TestDurakSocketsAndGetSessionNeverLeakTheOtherHand(t *testing.T) {
	s := newStand(t)
	ctx := t.Context()
	a, b, stranger := uuid.New(), uuid.New(), uuid.New()
	created, err := s.client.CreateSession(ctx, asUser(a, &gamev1.CreateSessionRequest{Game: string(durak.Key), RequestId: uuid.NewString()}))
	if err != nil {
		t.Fatal(err)
	}
	id := created.Msg.GetSession().GetId()
	joined, err := s.client.JoinSession(ctx, asUser(b, &gamev1.JoinSessionRequest{SessionId: id}))
	if err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.MustParse(id)
	full := func() session.Session {
		game, err := s.owner.Get(ctx, sessionID, a)
		if err != nil {
			t.Fatal(err)
		}
		return game
	}
	history := map[uint32]engine.State{}
	current := full()
	history[current.Revision] = current.State
	raw, _ := protojson.Marshal(joined.Msg.GetSession())
	checkHidden(t, "join response", raw, joined.Msg.GetSession(), current.State, 1)
	users := []uuid.UUID{a, b}
	sockets := []*watcher{s.watch(t, a, id), s.watch(t, b, id)}
	spectator := s.watch(t, stranger, id)
	type seen struct {
		frame
		seat int
	}
	var pending []seen
	checked := 0
	flush := func() {
		t.Helper()
		for _, p := range pending {
			state, ok := history[p.session.GetRevision()]
			if !ok {
				t.Fatalf("frame at unknown revision %d", p.session.GetRevision())
			}
			checkHidden(t, fmt.Sprintf("socket frame %d for seat %d", p.session.GetRevision(), p.seat), p.raw, p.session, state, p.seat)
			checked++
		}
		pending = pending[:0]
	}
	until := func(w *watcher, seat int, revision uint32) {
		t.Helper()
		for {
			f := w.next()
			if f.session == nil {
				t.Fatalf("seat %d: unexpected error %v", seat, f.err)
			}
			pending = append(pending, seen{f, seat})
			if f.session.GetRevision() >= revision {
				return
			}
		}
	}
	expect := func(w *watcher, seat int, want gamev1.SessionError) {
		t.Helper()
		for {
			f := w.next()
			if f.session != nil {
				pending = append(pending, seen{f, seat})
				continue
			}
			if f.err != want {
				t.Fatalf("error %v, want %v", f.err, want)
			}
			return
		}
	}
	until(spectator, engine.NoSeat, current.Revision)
	mover := current.State.Turn
	sockets[mover].send(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_DurakMove{DurakMove: &gamev1.DurakMove{Ply: 0, Move: "attack 1X"}}})
	expect(sockets[mover], mover, gamev1.SessionError_SESSION_ERROR_ILLEGAL_MOVE)
	sockets[mover].send(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_DurakMove{DurakMove: &gamev1.DurakMove{Ply: 0, Move: strings.Repeat("x", 64)}}})
	expect(sockets[mover], mover, gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND)
	spectator.send(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_DurakMove{DurakMove: &gamev1.DurakMove{Ply: 0, Move: "take"}}})
	expect(spectator, engine.NoSeat, gamev1.SessionError_SESSION_ERROR_SPECTATOR)
	for !current.State.Over() {
		seat := current.State.Turn
		legal := durak.New().ValidMoves(current.State, current.State.Players[seat])
		pick := legal[(int(current.Revision)*7)%len(legal)]
		revision := current.Revision
		if current.State.Ply < 6 {
			var move durak.MoveData
			_ = json.Unmarshal(pick.Data, &move)
			sockets[seat].send(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_DurakMove{DurakMove: &gamev1.DurakMove{Ply: uint32(current.State.Ply), Move: move.Move, Revision: &revision}}})
			until(sockets[seat], seat, revision+1)
			current = full()
		} else {
			next, err := s.owner.Move(ctx, sessionID, users[seat], current.State.Ply, durak.Key, pick.Data, &revision)
			if err != nil {
				t.Fatal(err)
			}
			current = next
		}
		history[current.Revision] = current.State
		until(spectator, engine.NoSeat, current.Revision)
		flush()
		if current.State.Ply == 3 {
			sockets[seat].send(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_Undo{Undo: &gamev1.UndoMove{Revision: &current.Revision}}})
			expect(sockets[seat], seat, gamev1.SessionError_SESSION_ERROR_UNDO_DISABLED)
		}
		if current.State.Ply%9 == 1 && !current.State.Over() {
			for seat, user := range []uuid.UUID{a, b, stranger} {
				got, err := s.client.GetSession(ctx, asUser(user, &gamev1.GetSessionRequest{SessionId: id}))
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := protojson.Marshal(got.Msg.GetSession())
				if seat == 2 {
					seat = engine.NoSeat
				}
				checkHidden(t, fmt.Sprintf("GetSession at ply %d", current.State.Ply), raw, got.Msg.GetSession(), current.State, seat)
				checked++
			}
		}
	}
	for seat, w := range sockets {
		until(w, seat, current.Revision)
	}
	flush()
	if checked < current.State.Ply {
		t.Fatalf("only %d frames checked over %d plies", checked, current.State.Ply)
	}
	end, err := s.client.GetSession(ctx, asUser(stranger, &gamev1.GetSessionRequest{SessionId: id}))
	if err != nil || end.Msg.GetSession().GetDurak().GetEnd() == gamev1.DurakEnd_DURAK_END_UNSPECIFIED {
		t.Fatal("finished game", end, err)
	}
}

func TestDurakHistoryIsRedactedWhileLiveAndRevealedAfterwards(t *testing.T) {
	games := cardGames(t)
	rules := session.NewRules(games)
	h := New(Config{Store: session.NewStore(nil, games)})
	a, b, stranger := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	game, _, err := rules.Start(uuid.New(), durak.Key, a, b, false, now)
	if err != nil {
		t.Fatal(err)
	}
	users := map[engine.PlayerID]uuid.UUID{engine.PlayerID(a.String()): a, engine.PlayerID(b.String()): b}
	var moves []session.MoveRecord
	var states []engine.State
	states = append(states, game.State)
	for !game.State.Over() {
		player := game.State.Players[game.State.Turn]
		legal := durak.New().ValidMoves(game.State, player)
		next, change, err := rules.Move(game, users[player], game.State.Ply, durak.Key, legal[len(legal)/2].Data, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		moves = append(moves, session.MoveRecord{Ply: change.Move.Ply, Player: users[player], Data: change.Move.Data, PlayedAt: now})
		game = next
		states = append(states, game.State)
		if len(moves) == 10 {
			live := game
			for seat, viewer := range []uuid.UUID{a, b, stranger} {
				if seat == 2 {
					seat = engine.NoSeat
				}
				response, err := replayFor(t, h, rules, live, moves, viewer)
				if err != nil {
					t.Fatal(err)
				}
				if len(response.GetFrames()) != len(moves)+1 {
					t.Fatal("frames", len(response.GetFrames()))
				}
				for i, f := range response.GetFrames() {
					raw, _ := protojson.Marshal(f)
					board := f.GetDurak()
					for s, hand := range board.GetHands() {
						if hand.GetHidden() != (s != seat) {
							t.Fatalf("live history frame %d: seat %d sees seat %d as %+v", i, seat, s, hand)
						}
					}
					if len(board.GetLegalMoves()) != 0 {
						t.Fatal("history frames carry legal moves")
					}
					if found := leaked(raw, secrets(t, states[i], seat)); len(found) > 0 {
						t.Fatalf("live history frame %d leaked %v to seat %d", i, found, seat)
					}
				}
				raw, _ := protojson.Marshal(response.GetSession())
				checkHidden(t, "live history session", raw, response.GetSession(), live.State, seat)
			}
		}
	}
	response, err := replayFor(t, h, rules, game, moves, stranger)
	if err != nil {
		t.Fatal(err)
	}
	middle := response.GetFrames()[len(moves)/2].GetDurak()
	for s, hand := range middle.GetHands() {
		if hand.GetHidden() || len(hand.GetCards()) != int(hand.GetCount()) {
			t.Fatalf("finished replay hides seat %d", s)
		}
	}
	if raw, _ := protojson.Marshal(response); strings.Contains(string(raw), "seed") {
		t.Fatal("the seed left the service")
	}
}

func replayFor(t *testing.T, h *Handler, rules session.Rules, game session.Session, moves []session.MoveRecord, viewer uuid.UUID) (*gamev1.GetSessionHistoryResponse, error) {
	t.Helper()
	frames, err := buildHistory(t.Context(), rules, game, moves, 0, h.seen(game, viewer))
	if err != nil {
		return nil, err
	}
	state, err := h.view(t.Context(), game, viewer, nil)
	if err != nil {
		return nil, err
	}
	return &gamev1.GetSessionHistoryResponse{Session: state, Frames: frames}, nil
}

func TestDurakListsRedactForTheCaller(t *testing.T) {
	games := cardGames(t)
	h := New(Config{Store: session.NewStore(nil, games)})
	a, b := uuid.New(), uuid.New()
	game, _, err := session.NewRules(games).Start(uuid.New(), durak.Key, a, b, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for seat, viewer := range []uuid.UUID{a, b, uuid.New(), uuid.Nil} {
		out, err := h.result(game, viewer, nil)
		if err != nil {
			t.Fatal(err)
		}
		if seat > 1 {
			seat = engine.NoSeat
		}
		raw, _ := protojson.Marshal(out)
		checkHidden(t, "list item", raw, out, game.State, seat)
	}
	waiting, err := h.result(session.Session{ID: uuid.New(), Game: durak.Key, Creator: a, UpdatedAt: time.Now()}, b, nil)
	if err != nil || waiting.GetDurak() == nil || len(waiting.GetDurak().GetHands()) != 0 {
		t.Fatal("waiting game", waiting, err)
	}
}

func TestDurakMoveErrors(t *testing.T) {
	for _, err := range []error{durak.ErrIllegalMove, durak.ErrBadMove} {
		if got := moveError(fmt.Errorf("session: %w", err)); got != gamev1.SessionError_SESSION_ERROR_ILLEGAL_MOVE {
			t.Errorf("moveError(%v) = %v", err, got)
		}
	}
	if got := moveError(session.ErrNoUndo); got != gamev1.SessionError_SESSION_ERROR_UNDO_DISABLED {
		t.Errorf("moveError(ErrNoUndo) = %v", got)
	}
}
