package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/durak"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

func (s *stand) start(t *testing.T, game engine.Key) string {
	t.Helper()
	created, err := s.client.CreateSession(t.Context(), asUser(uuid.New(), &gamev1.CreateSessionRequest{Game: string(game), RequestId: uuid.NewString()}))
	if err != nil {
		t.Fatal(err)
	}
	id := created.Msg.GetSession().GetId()
	if _, err := s.client.JoinSession(t.Context(), asUser(uuid.New(), &gamev1.JoinSessionRequest{SessionId: id})); err != nil {
		t.Fatal(err)
	}
	return id
}

func (s *stand) full(t *testing.T, id string) session.Session {
	t.Helper()
	game, err := s.owner.Get(t.Context(), uuid.MustParse(id), uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	return game
}

func (s *stand) play(t *testing.T, id string, rules engine.Game, key engine.Key) session.Session {
	t.Helper()
	current := s.full(t, id)
	player := current.State.Players[current.State.Turn]
	revision := current.Revision
	next, err := s.owner.Move(t.Context(), current.ID, uuid.MustParse(string(player)), current.State.Ply, key, rules.ValidMoves(current.State, player)[0].Data, &revision)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func (s *stand) feed(t *testing.T, user uuid.UUID, ids ...string) *watcher {
	t.Helper()
	return s.open(t, user, watchCommand(ids...))
}

func watchCommand(ids ...string) *gamev1.SessionCommand {
	return &gamev1.SessionCommand{Action: &gamev1.SessionCommand_Watch{Watch: &gamev1.WatchSessions{SessionIds: ids}}}
}

func (w *watcher) reach(id string, revision uint32) []frame {
	w.t.Helper()
	var seen []frame
	for {
		f := w.next()
		if f.session == nil {
			w.t.Fatalf("unexpected error %v", f.err)
		}
		seen = append(seen, f)
		if f.session.GetId() == id && f.session.GetRevision() >= revision {
			return seen
		}
	}
}

func (w *watcher) failure(want gamev1.SessionError) {
	w.t.Helper()
	for {
		f := w.next()
		if f.session != nil {
			continue
		}
		if f.err != want {
			w.t.Fatalf("error %v, want %v", f.err, want)
		}
		return
	}
}

func TestFeedStreamsSeveralGamesToAReadOnlyViewer(t *testing.T) {
	s := newStand(t)
	board, cards := s.start(t, tictactoe.Key), s.start(t, durak.Key)
	viewer := s.feed(t, uuid.New(), board, cards)
	history := map[uint32]engine.State{}
	current := s.full(t, cards)
	history[current.Revision] = current.State
	check := func(frames []frame) {
		t.Helper()
		for _, f := range frames {
			if f.session.GetId() != cards {
				continue
			}
			state, ok := history[f.session.GetRevision()]
			if !ok {
				t.Fatalf("durak frame at unknown revision %d", f.session.GetRevision())
			}
			checkHidden(t, fmt.Sprintf("feed frame %d", f.session.GetRevision()), f.raw, f.session, state, engine.NoSeat)
			if f.session.GetStatus() == gamev1.SessionStatus_SESSION_STATUS_PLAYING && len(f.session.GetDurak().GetLegalMoves()) > 0 {
				t.Fatalf("a feed viewer got legal moves %v", f.session.GetDurak().GetLegalMoves())
			}
		}
	}
	first := viewer.reach(board, 0)
	if first[0].session.GetGame() != string(tictactoe.Key) || first[0].session.GetSpectators() != 1 {
		t.Fatalf("first frame %+v", first[0].session)
	}
	check(viewer.reach(cards, current.Revision))

	moved := s.play(t, board, tictactoe.New(), tictactoe.Key)
	if got := viewer.reach(board, moved.Revision); got[len(got)-1].session.GetPly() != 1 {
		t.Fatalf("tic-tac-toe frame at ply %d", got[len(got)-1].session.GetPly())
	}
	for range 4 {
		current = s.play(t, cards, durak.New(), durak.Key)
		history[current.Revision] = current.State
		check(viewer.reach(cards, current.Revision))
	}

	viewer.send(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_Move{Move: &gamev1.TicTacToeMove{Ply: 1, Cell: 4}}})
	viewer.failure(gamev1.SessionError_SESSION_ERROR_SPECTATOR)
	viewer.send(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_Subscribe{Subscribe: board}})
	viewer.failure(gamev1.SessionError_SESSION_ERROR_SPECTATOR)
	if after := s.full(t, board); after.Revision != moved.Revision {
		t.Fatalf("a feed viewer changed the game to revision %d", after.Revision)
	}
}

func TestFeedSwitchesGamesAndReleasesTheOldOnes(t *testing.T) {
	s := newStand(t)
	first, second := s.start(t, tictactoe.Key), s.start(t, tictactoe.Key)
	owner := s.full(t, first)
	player := s.watch(t, uuid.MustParse(string(owner.State.Players[0])), first)
	viewer := s.feed(t, uuid.New(), first)
	viewer.reach(first, owner.Revision)
	waitSpectators(t, player, 1)

	viewer.send(watchCommand(second))
	viewer.reach(second, 0)
	waitSpectators(t, player, 0)

	s.play(t, first, tictactoe.New(), tictactoe.Key)
	moved := s.play(t, second, tictactoe.New(), tictactoe.Key)
	for _, f := range viewer.reach(second, moved.Revision) {
		if f.session.GetId() == first {
			t.Fatal("a released game still streams to the feed")
		}
	}
}

func waitSpectators(t *testing.T, w *watcher, want uint32) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-w.frames:
			if !ok {
				t.Fatal("socket closed")
			}
			if f.session != nil && f.session.GetSpectators() == want {
				return
			}
		case <-deadline:
			t.Fatalf("spectators never reached %d", want)
		}
	}
}

func TestFeedRejectsMalformedWatches(t *testing.T) {
	s := newStand(t)
	id := s.start(t, tictactoe.Key)
	cases := map[string][]string{
		"too many games": {id, uuid.NewString(), uuid.NewString(), uuid.NewString()},
		"not an id":      {"nope"},
		"nil id":         {uuid.Nil.String()},
	}
	for name, ids := range cases {
		t.Run(name, func(t *testing.T) {
			s.feed(t, uuid.New(), ids...).failure(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND)
		})
	}
	viewer := s.feed(t, uuid.New(), id)
	viewer.reach(id, 0)
	viewer.send(watchCommand("nope"))
	viewer.failure(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND)
	viewer.send(watchCommand(id, id))
	moved := s.play(t, id, tictactoe.New(), tictactoe.Key)
	viewer.reach(id, moved.Revision)
}

func TestFeedSkipsGamesNobodyJoinedYet(t *testing.T) {
	s := newStand(t)
	created, err := s.client.CreateSession(t.Context(), asUser(uuid.New(), &gamev1.CreateSessionRequest{Game: string(tictactoe.Key), RequestId: uuid.NewString()}))
	if err != nil {
		t.Fatal(err)
	}
	open, live := created.Msg.GetSession().GetId(), s.start(t, tictactoe.Key)
	for _, f := range s.feed(t, uuid.New(), open, live).reach(live, 0) {
		if f.session.GetId() == open {
			t.Fatal("the feed showed a game that is still waiting for an opponent")
		}
	}
}
