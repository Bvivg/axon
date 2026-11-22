package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/pkg/adminquery"
	"github.com/bvivg/axon/core/shared/pkg/authz"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

type fakeAdminStore struct {
	game    session.Session
	moves   []session.MoveRecord
	entries []session.QueueEntry
	total   *int64
	err     error

	askedID    uuid.UUID
	askedLimit int
	askedGame  engine.Key
	askedList  adminquery.Request
	left       []uuid.UUID
}

func (s *fakeAdminStore) AdminSession(_ context.Context, id uuid.UUID, limit int) (session.Session, []session.MoveRecord, error) {
	s.askedID, s.askedLimit = id, limit
	return s.game, s.moves, s.err
}

func (s *fakeAdminStore) MatchQueue(_ context.Context, f session.QueueFilter) (session.QueuePage, error) {
	s.askedGame, s.askedLimit, s.askedList = f.Game, f.Limit, f.List
	return session.QueuePage{Entries: s.entries, Total: s.total}, s.err
}

func (s *fakeAdminStore) LeaveQueue(_ context.Context, user uuid.UUID, game engine.Key) error {
	s.askedGame = game
	if s.err == nil {
		s.left = append(s.left, user)
	}
	return s.err
}

type recorded struct {
	actions []authz.Action
	err     error
}

func (r *recorded) record(_ context.Context, action authz.Action) error {
	r.actions = append(r.actions, action)
	return r.err
}

func testAdmin(t *testing.T, store *fakeAdminStore, audit *recorded) *Admin {
	t.Helper()
	games := engine.NewRegistry()
	if err := games.Register(tictactoe.Definition()); err != nil {
		t.Fatal(err)
	}
	return &Admin{h: New(Config{Store: session.NewStore(nil, games)}), store: store, record: audit.record, log: logger.Discard()}
}

func TestAdminGetGameSession(t *testing.T) {
	x, o := uuid.New(), uuid.New()
	game := session.Session{ID: uuid.New(), Creator: x, Opponent: &o, Game: tictactoe.Key, Revision: 3, UpdatedAt: time.Now(),
		State: tictactoe.New().Init([]engine.PlayerID{engine.PlayerID(x.String()), engine.PlayerID(o.String())})}
	played := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	moves := []session.MoveRecord{
		{Ply: 1, Player: x, Data: json.RawMessage(`{"row":1,"col":1}`), PlayedAt: played},
		{Ply: 2, Player: o, Data: json.RawMessage(`{"row":0,"col":0}`), PlayedAt: played.Add(time.Second)},
	}
	tests := []struct {
		name  string
		id    string
		store *fakeAdminStore
		code  connect.Code
	}{
		{name: "session with its moves", id: game.ID.String(), store: &fakeAdminStore{game: game, moves: moves}},
		{name: "malformed id", id: "nope", store: &fakeAdminStore{}, code: connect.CodeInvalidArgument},
		{name: "nil id", id: uuid.Nil.String(), store: &fakeAdminStore{}, code: connect.CodeInvalidArgument},
		{name: "missing session", id: game.ID.String(), store: &fakeAdminStore{err: fmt.Errorf("session: admin session: %w", session.ErrNotFound)}, code: connect.CodeNotFound},
		{name: "store down", id: game.ID.String(), store: &fakeAdminStore{err: errors.New("postgres down")}, code: connect.CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := testAdmin(t, tt.store, &recorded{}).GetGameSession(context.Background(), connect.NewRequest(&gamev1.GetGameSessionRequest{SessionId: tt.id}))
			if tt.code != 0 {
				if connect.CodeOf(err) != tt.code {
					t.Fatalf("code = %v, want %v (%v)", connect.CodeOf(err), tt.code, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.store.askedID != game.ID || tt.store.askedLimit != session.AdminMoveLimit {
				t.Fatalf("asked %s with limit %d", tt.store.askedID, tt.store.askedLimit)
			}
			out := response.Msg
			if out.Session.GetId() != game.ID.String() || out.Session.GetGame() != string(tictactoe.Key) || out.Session.GetRevision() != 3 ||
				out.Session.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_PLAYING || len(out.Moves) != 2 {
				t.Fatalf("response = %v", out)
			}
			for i, move := range out.Moves {
				want := moves[i]
				if int(move.Ply) != want.Ply || move.PlayerId != want.Player.String() || move.Data != string(want.Data) || !move.PlayedAt.AsTime().Equal(want.PlayedAt) {
					t.Fatalf("move %d = %v, want %+v", i, move, want)
				}
			}
		})
	}
}

func TestAdminListMatchQueue(t *testing.T) {
	waiting, matched, game := uuid.New(), uuid.New(), uuid.New()
	joined := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	entries := []session.QueueEntry{
		{User: waiting, Game: tictactoe.Key, Rating: 1210, JoinedAt: joined, LastSeenAt: joined.Add(5 * time.Second)},
		{User: matched, Game: tictactoe.Key, Rating: 1190, JoinedAt: joined.Add(-time.Minute), LastSeenAt: joined, Session: &game},
	}
	total, offset := int64(42), int32(20)
	tests := []struct {
		name    string
		request *gamev1.ListMatchQueueRequest
		store   *fakeAdminStore
		code    connect.Code
		list    adminquery.Request
	}{
		{name: "every game", request: &gamev1.ListMatchQueueRequest{}, store: &fakeAdminStore{entries: entries}},
		{name: "one game", request: &gamev1.ListMatchQueueRequest{Game: string(tictactoe.Key)}, store: &fakeAdminStore{entries: entries}},
		{
			name:    "a filtered page carries the query and the total",
			request: &gamev1.ListMatchQueueRequest{Filter: proto.String("matched = false"), OrderBy: proto.String("rating desc"), Offset: &offset, PageSize: 10},
			store:   &fakeAdminStore{entries: entries, total: &total},
			list:    adminquery.Request{Filter: "matched = false", OrderBy: "rating desc", Offset: &offset, PageSize: 10},
		},
		{name: "unknown game", request: &gamev1.ListMatchQueueRequest{Game: "go"}, store: &fakeAdminStore{}, code: connect.CodeInvalidArgument},
		{name: "invalid query", request: &gamev1.ListMatchQueueRequest{Filter: proto.String("colour = red")}, store: &fakeAdminStore{err: &adminquery.Error{Field: "filter", Message: "cannot filter by \"colour\""}}, code: connect.CodeInvalidArgument},
		{name: "store down", request: &gamev1.ListMatchQueueRequest{}, store: &fakeAdminStore{err: errors.New("postgres down")}, code: connect.CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := testAdmin(t, tt.store, &recorded{}).ListMatchQueue(context.Background(), connect.NewRequest(tt.request))
			if tt.code != 0 {
				if connect.CodeOf(err) != tt.code {
					t.Fatalf("code = %v, want %v (%v)", connect.CodeOf(err), tt.code, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(tt.store.askedGame) != tt.request.GetGame() || tt.store.askedLimit != session.AdminQueueLimit {
				t.Fatalf("asked %q with limit %d", tt.store.askedGame, tt.store.askedLimit)
			}
			if asked := tt.store.askedList; asked.Filter != tt.list.Filter || asked.OrderBy != tt.list.OrderBy || asked.PageSize != tt.list.PageSize ||
				asked.Paged() != tt.list.Paged() || asked.Paged() && *asked.Offset != *tt.list.Offset {
				t.Fatalf("asked list %+v, want %+v", asked, tt.list)
			}
			if (response.Msg.Total == nil) != (tt.store.total == nil) || tt.store.total != nil && response.Msg.GetTotal() != *tt.store.total {
				t.Fatalf("total = %v, want %v", response.Msg.Total, tt.store.total)
			}
			out := response.Msg.Entries
			if len(out) != 2 {
				t.Fatalf("entries = %v", out)
			}
			first, second := out[0], out[1]
			if first.UserId != waiting.String() || first.Game != string(tictactoe.Key) || first.Rating != 1210 || first.SessionId != "" ||
				!first.JoinedAt.AsTime().Equal(joined) || !first.LastSeenAt.AsTime().Equal(joined.Add(5*time.Second)) {
				t.Fatalf("waiting entry = %v", first)
			}
			if second.UserId != matched.String() || second.SessionId != game.String() {
				t.Fatalf("matched entry = %v", second)
			}
		})
	}
}

func TestAdminRemoveFromMatchQueue(t *testing.T) {
	admin, player := uuid.New(), uuid.New()
	valid := &gamev1.RemoveFromMatchQueueRequest{UserId: player.String(), Game: string(tictactoe.Key), Reason: "  stuck in the queue  "}
	with := func(change func(*gamev1.RemoveFromMatchQueueRequest)) *gamev1.RemoveFromMatchQueueRequest {
		out := &gamev1.RemoveFromMatchQueueRequest{UserId: valid.UserId, Game: valid.Game, Reason: valid.Reason}
		change(out)
		return out
	}
	tests := []struct {
		name    string
		request *gamev1.RemoveFromMatchQueueRequest
		store   *fakeAdminStore
		audit   *recorded
		code    connect.Code
		left    bool
	}{
		{name: "waiting player removed", request: valid, store: &fakeAdminStore{}, audit: &recorded{}, left: true},
		{name: "audit failure does not undo the removal", request: valid, store: &fakeAdminStore{}, audit: &recorded{err: errors.New("outbox down")}, left: true},
		{name: "malformed user", request: with(func(r *gamev1.RemoveFromMatchQueueRequest) { r.UserId = "player" }), store: &fakeAdminStore{}, audit: &recorded{}, code: connect.CodeInvalidArgument},
		{name: "nil user", request: with(func(r *gamev1.RemoveFromMatchQueueRequest) { r.UserId = uuid.Nil.String() }), store: &fakeAdminStore{}, audit: &recorded{}, code: connect.CodeInvalidArgument},
		{name: "blank reason", request: with(func(r *gamev1.RemoveFromMatchQueueRequest) { r.Reason = "  ok  " }), store: &fakeAdminStore{}, audit: &recorded{}, code: connect.CodeInvalidArgument},
		{name: "unknown game", request: with(func(r *gamev1.RemoveFromMatchQueueRequest) { r.Game = "go" }), store: &fakeAdminStore{}, audit: &recorded{}, code: connect.CodeInvalidArgument},
		{name: "not waiting", request: valid, store: &fakeAdminStore{err: session.ErrNotSearching}, audit: &recorded{}, code: connect.CodeNotFound},
		{name: "store down", request: valid, store: &fakeAdminStore{err: errors.New("postgres down")}, audit: &recorded{}, code: connect.CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := authz.WithActor(context.Background(), authz.Actor{UserID: admin})
			_, err := testAdmin(t, tt.store, tt.audit).RemoveFromMatchQueue(ctx, connect.NewRequest(tt.request))
			if (tt.code == 0) != (err == nil) || err != nil && connect.CodeOf(err) != tt.code {
				t.Fatalf("code = %v, want %v (%v)", connect.CodeOf(err), tt.code, err)
			}
			if tt.left != (len(tt.store.left) == 1 && tt.store.left[0] == player) {
				t.Fatalf("left = %v", tt.store.left)
			}
			if !tt.left {
				if len(tt.audit.actions) != 0 {
					t.Fatalf("recorded %+v for a failed removal", tt.audit.actions)
				}
				return
			}
			if tt.store.askedGame != tictactoe.Key || len(tt.audit.actions) != 1 {
				t.Fatalf("asked %q, recorded %+v", tt.store.askedGame, tt.audit.actions)
			}
			action := tt.audit.actions[0]
			if action.Service != "game" || action.Name != "queue_entry_removed" || action.TargetType != "user" || action.TargetID != player.String() ||
				action.Reason != "stuck in the queue" || action.ActorID != admin || len(action.Details) != 1 || action.Details["game"] != string(tictactoe.Key) {
				t.Fatalf("action = %+v", action)
			}
		})
	}
}
