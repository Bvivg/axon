//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

func feed(t *testing.T, who player, ids ...string) *socket {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	conn, err := ws.Dial(ctx, strings.Replace(gatewayURL(), "http", "ws", 1)+"/ws/game", ws.DialOptions{
		Subprotocols: []string{"axon.game.v1", "axon.bearer." + who.token},
	})
	if err != nil {
		t.Fatal("dial game socket:", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	s := &socket{t: t, conn: conn}
	s.send(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_Watch{Watch: &gamev1.WatchSessions{SessionIds: ids}}})
	return s
}

func pair(t *testing.T, game string) (string, player, player) {
	t.Helper()
	first, second := register(t), register(t)
	created, err := games().CreateSession(t.Context(), as(first, &gamev1.CreateSessionRequest{Game: game, RequestId: uuid.NewString()}))
	if err != nil {
		t.Fatal("create:", err)
	}
	id := created.Msg.GetSession().GetId()
	if _, err := games().JoinSession(t.Context(), as(second, &gamev1.JoinSessionRequest{SessionId: id})); err != nil {
		t.Fatal("join:", err)
	}
	return id, first, second
}

func (s *socket) game(id string, match func(*gamev1.Session) bool, into *[]received) *gamev1.Session {
	s.t.Helper()
	for {
		raw, event := s.frame()
		if event.GetError() != gamev1.SessionError_SESSION_ERROR_UNSPECIFIED {
			s.t.Fatalf("feed error %v", event.GetError())
		}
		current := event.GetSession()
		if current == nil {
			continue
		}
		if into != nil && current.GetId() == id {
			*into = append(*into, received{raw: raw, session: current, seat: -1})
		}
		if current.GetId() == id && match(current) {
			return current
		}
	}
}

func TestTVFeedStreamsGamesOfDifferentKindsThroughTheGateway(t *testing.T) {
	ctx := t.Context()
	board, x, o := pair(t, "tictactoe")
	cards, north, south := pair(t, "durak")
	viewer := register(t)

	eventually(t, "both games in the mixed live list", func() bool {
		live, err := games().ListLiveSessions(ctx, as(viewer, &gamev1.ListLiveSessionsRequest{}))
		if err != nil {
			t.Fatal("live list:", err)
		}
		kinds := map[string]string{}
		for _, item := range live.Msg.GetSessions() {
			kinds[item.GetId()] = item.GetGame()
		}
		return kinds[board] == "tictactoe" && kinds[cards] == "durak"
	})

	tv := feed(t, viewer, board, cards)
	var frames []received
	hands := map[uint32][2][]string{}
	if first := tv.game(board, func(*gamev1.Session) bool { return true }, nil); first.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_PLAYING {
		t.Fatalf("tic-tac-toe on the feed = %v", first)
	}
	tv.game(cards, func(*gamev1.Session) bool { return true }, &frames)

	crosses, noughts := open(t, x, board), open(t, o, board)
	crosses.atPly(0)
	noughts.atPly(0)
	var seen *gamev1.Session
	for i, cell := range []uint32{4, 0, 1, 2, 7} {
		mover := []*socket{crosses, noughts}[i%2]
		if i > 0 {
			mover.atPly(uint32(i))
		}
		mover.send(move(uint32(i), cell))
		seen = tv.game(board, func(s *gamev1.Session) bool { return s.GetPly() == uint32(i+1) }, nil)
		if seen.GetTicTacToe().GetCells()[cell] == gamev1.Mark_MARK_UNSPECIFIED {
			t.Fatalf("the feed missed the move into cell %d: %v", cell, seen.GetTicTacToe().GetCells())
		}
	}
	if seen.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_WON || seen.GetWinnerUserId() != x.id {
		t.Fatalf("the feed showed the end as %v won by %q", seen.GetStatus(), seen.GetWinnerUserId())
	}

	sockets := []*socket{open(t, north, cards), open(t, south, cards)}
	views := make([]*gamev1.Session, 2)
	var ignored []received
	sync := func(revision uint32, feed bool) {
		t.Helper()
		for seat, s := range sockets {
			views[seat] = s.collect(seat, revision, &ignored)
		}
		hands[views[0].GetRevision()] = [2][]string{views[0].GetDurak().GetHands()[0].GetCards(), views[1].GetDurak().GetHands()[1].GetCards()}
		if feed {
			tv.game(cards, func(s *gamev1.Session) bool { return s.GetRevision() >= revision }, &frames)
		}
	}
	start, err := games().GetSession(ctx, as(north, &gamev1.GetSessionRequest{SessionId: cards}))
	if err != nil {
		t.Fatal("get:", err)
	}
	sync(start.Msg.GetSession().GetRevision(), false)
	for range 6 {
		seat := slices.Index([]string{north.id, south.id}, views[0].GetTurnUserId())
		if seat < 0 {
			break
		}
		time.Sleep(300 * time.Millisecond)
		sockets[seat].send(durakMove(views[seat], views[seat].GetDurak().GetLegalMoves()[0]))
		sync(views[seat].GetRevision()+1, true)
	}
	if len(frames) < 4 {
		t.Fatalf("the feed relayed only %d durak frames", len(frames))
	}
	for _, got := range frames {
		if _, ok := hands[got.session.GetRevision()]; !ok {
			continue
		}
		assertRedacted(t, fmt.Sprintf("feed frame at revision %d", got.session.GetRevision()), got, hands)
		for seat, hand := range got.session.GetDurak().GetHands() {
			if got.session.GetStatus() == gamev1.SessionStatus_SESSION_STATUS_PLAYING && (!hand.GetHidden() || len(hand.GetCards()) > 0) {
				t.Fatalf("the feed revealed seat %d: %v", seat, hand)
			}
		}
	}

	tv.send(durakMove(views[0], "take"))
	tv.until("feed refusal", func(event *gamev1.SessionEvent) bool {
		return event.GetError() == gamev1.SessionError_SESSION_ERROR_SPECTATOR
	})
}
