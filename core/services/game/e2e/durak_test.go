//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

type received struct {
	raw     []byte
	session *gamev1.Session
	seat    int
}

func (s *socket) frame() ([]byte, *gamev1.SessionEvent) {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(s.t.Context(), 20*time.Second)
	defer cancel()
	raw, err := s.conn.Read(ctx)
	if err != nil {
		s.t.Fatal("socket read:", err)
	}
	var event gamev1.SessionEvent
	if err := protojson.Unmarshal(raw, &event); err != nil {
		s.t.Fatal(err)
	}
	return raw, &event
}

func (s *socket) collect(seat int, revision uint32, into *[]received) *gamev1.Session {
	s.t.Helper()
	for {
		raw, event := s.frame()
		if event.GetError() != gamev1.SessionError_SESSION_ERROR_UNSPECIFIED {
			s.t.Fatalf("seat %d got error %v", seat, event.GetError())
		}
		current := event.GetSession()
		if current == nil {
			continue
		}
		*into = append(*into, received{raw: raw, session: current, seat: seat})
		if current.GetRevision() >= revision {
			return current
		}
	}
}

func durakMove(game *gamev1.Session, move string) *gamev1.SessionCommand {
	revision := game.GetRevision()
	return &gamev1.SessionCommand{Action: &gamev1.SessionCommand_DurakMove{DurakMove: &gamev1.DurakMove{Ply: game.GetPly(), Move: move, Revision: &revision}}}
}

func public(board *gamev1.DurakBoard) map[string]bool {
	out := map[string]bool{board.GetTrump(): true}
	for _, move := range board.GetMoves() {
		for _, word := range strings.Fields(move)[1:] {
			out[word] = true
		}
	}
	return out
}

func mentions(raw []byte, card string) bool {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_])` + card + `([^A-Za-z0-9_]|$)`).Match(raw)
}

func assertRedacted(t *testing.T, where string, got received, hands map[uint32][2][]string) {
	t.Helper()
	board := got.session.GetDurak()
	if got.session.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_PLAYING {
		return
	}
	if strings.Contains(string(got.raw), "seed") {
		t.Fatalf("%s: the seed reached seat %d: %s", where, got.seat, got.raw)
	}
	for s, hand := range board.GetHands() {
		if s == got.seat {
			continue
		}
		if !hand.GetHidden() || len(hand.GetCards()) != 0 {
			t.Fatalf("%s: seat %d received the hand of seat %d: %v", where, got.seat, s, hand)
		}
	}
	if len(board.GetLegalMoves()) > 0 && got.session.GetTurnUserId() != "" && got.seat < 0 {
		t.Fatalf("%s: a spectator received legal moves %v", where, board.GetLegalMoves())
	}
	known, ok := hands[got.session.GetRevision()]
	if !ok {
		t.Fatalf("%s: no record of revision %d", where, got.session.GetRevision())
	}
	open := public(board)
	for s, hand := range known {
		if s == got.seat {
			continue
		}
		for _, card := range hand {
			if !open[card] && mentions(got.raw, card) {
				t.Fatalf("%s: seat %d received %s from the hand of seat %d: %s", where, got.seat, card, s, got.raw)
			}
		}
	}
}

func TestDurakPlayedThroughTheGatewayNeverRevealsAHand(t *testing.T) {
	ctx := t.Context()
	client := games()
	north, south, watcher := register(t), register(t), register(t)
	created, err := client.CreateSession(ctx, as(north, &gamev1.CreateSessionRequest{Game: "durak", RequestId: uuid.NewString()}))
	if err != nil {
		t.Fatal("create:", err)
	}
	id := created.Msg.GetSession().GetId()
	joined, err := client.JoinSession(ctx, as(south, &gamev1.JoinSessionRequest{SessionId: id}))
	if err != nil {
		t.Fatal("join:", err)
	}
	players := []player{north, south}
	sockets := []*socket{open(t, north, id), open(t, south, id)}
	spectator := open(t, watcher, id)
	var frames []received
	hands := map[uint32][2][]string{}
	views := make([]*gamev1.Session, 2)
	sync := func(revision uint32) {
		t.Helper()
		for seat, s := range sockets {
			views[seat] = s.collect(seat, revision, &frames)
		}
		spectator.collect(-1, revision, &frames)
		known := [2][]string{}
		for seat, view := range views {
			known[seat] = view.GetDurak().GetHands()[seat].GetCards()
		}
		hands[views[0].GetRevision()] = known
	}
	sync(joined.Msg.GetSession().GetRevision())
	actions := map[string]int{}
	last := map[int]time.Time{}
	for views[0].GetStatus() == gamev1.SessionStatus_SESSION_STATUS_PLAYING {
		seat := slices.Index([]string{north.id, south.id}, views[0].GetTurnUserId())
		if seat < 0 {
			t.Fatalf("turn of %q", views[0].GetTurnUserId())
		}
		mine := views[seat]
		legal := mine.GetDurak().GetLegalMoves()
		if len(legal) == 0 {
			t.Fatalf("seat %d to act with no legal moves at ply %d", seat, mine.GetPly())
		}
		if other := views[1-seat].GetDurak().GetLegalMoves(); len(other) > 0 {
			t.Fatalf("the waiting seat received legal moves %v", other)
		}
		choice := legal[0]
		switch {
		case mine.GetPly()%7 == 3 && slices.Contains(legal, "take"):
			choice = "take"
		case mine.GetPly()%5 == 2 && slices.Contains(legal, "pass"):
			choice = "pass"
		}
		actions[strings.Fields(choice)[0]]++
		if wait := 300*time.Millisecond - time.Since(last[seat]); wait > 0 {
			time.Sleep(wait)
		}
		sockets[seat].send(durakMove(mine, choice))
		last[seat] = time.Now()
		sync(mine.GetRevision() + 1)
		if views[0].GetPly() == 24 {
			assertLiveViews(t, id, players, watcher, views[0])
		}
	}
	for _, got := range frames {
		assertRedacted(t, fmt.Sprintf("frame at revision %d", got.session.GetRevision()), got, hands)
	}
	for _, action := range []string{"attack", "defend", "take", "pass"} {
		if actions[action] == 0 {
			t.Fatalf("the game never used %q: %v", action, actions)
		}
	}
	final := views[0]
	if final.GetDurak().GetEnd() == gamev1.DurakEnd_DURAK_END_UNSPECIFIED {
		t.Fatalf("finished without an end reason: %v", final.GetDurak())
	}
	var history *gamev1.GetSessionHistoryResponse
	eventually(t, "the finished history", func() bool {
		response, ok := historyOf(t, watcher, id)
		history = response
		return ok && history.GetSession().GetStatus() != gamev1.SessionStatus_SESSION_STATUS_PLAYING && len(history.GetFrames()) == int(final.GetPly())+1
	})
	for _, hand := range history.GetFrames()[len(history.GetFrames())/2].GetDurak().GetHands() {
		if hand.GetHidden() || len(hand.GetCards()) != int(hand.GetCount()) {
			t.Fatalf("the finished replay hides a hand: %v", hand)
		}
	}
	if raw, _ := protojson.Marshal(history); strings.Contains(string(raw), "seed") {
		t.Fatal("the seed reached the replay")
	}
	t.Logf("durak finished after %d plies with %v: %v", final.GetPly(), final.GetDurak().GetEnd(), actions)
}

func historyOf(t *testing.T, who player, id string) (*gamev1.GetSessionHistoryResponse, bool) {
	t.Helper()
	response, err := games().GetSessionHistory(t.Context(), as(who, &gamev1.GetSessionHistoryRequest{SessionId: id}))
	if connect.CodeOf(err) == connect.CodeResourceExhausted {
		return nil, false
	}
	if err != nil {
		t.Fatal("history:", err)
	}
	return response.Msg, true
}

func assertLiveViews(t *testing.T, id string, players []player, watcher player, current *gamev1.Session) {
	t.Helper()
	client := games()
	eventually(t, "the live history", func() bool {
		response, ok := historyOf(t, watcher, id)
		return ok && len(response.GetFrames()) > 1
	})
	for seat, who := range append(slices.Clone(players), watcher) {
		if seat == 2 {
			seat = -1
		}
		var response *gamev1.GetSessionHistoryResponse
		eventually(t, "the live history of a viewer", func() bool {
			got, ok := historyOf(t, who, id)
			response = got
			return ok
		})
		for i, frame := range response.GetFrames() {
			for s, hand := range frame.GetDurak().GetHands() {
				if s != seat && (!hand.GetHidden() || len(hand.GetCards()) != 0) {
					t.Fatalf("live history frame %d gave seat %d the hand of seat %d", i, seat, s)
				}
			}
		}
		got, err := client.GetSession(t.Context(), as(who, &gamev1.GetSessionRequest{SessionId: id}))
		if err != nil {
			t.Fatal("get:", err)
		}
		for s, hand := range got.Msg.GetSession().GetDurak().GetHands() {
			if s != seat && (!hand.GetHidden() || len(hand.GetCards()) != 0) {
				t.Fatalf("GetSession gave seat %d the hand of seat %d", seat, s)
			}
		}
	}
	live, err := client.ListLiveSessions(t.Context(), as(watcher, &gamev1.ListLiveSessionsRequest{Game: new("durak")}))
	if err != nil {
		t.Fatal("live list:", err)
	}
	for _, item := range live.Msg.GetSessions() {
		for s, hand := range item.GetDurak().GetHands() {
			if !hand.GetHidden() || len(hand.GetCards()) != 0 {
				t.Fatalf("the live list showed seat %d to a spectator", s)
			}
		}
	}
	_ = current
}
