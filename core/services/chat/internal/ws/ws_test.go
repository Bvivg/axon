package ws_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	sharedws "github.com/bvivg/axon/core/shared/pkg/ws"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/pubsub"
	"github.com/bvivg/axon/core/services/chat/internal/service"
	"github.com/bvivg/axon/core/services/chat/internal/ws"
)

type fakeRooms struct {
	mu sync.Mutex

	members map[uuid.UUID]map[uuid.UUID]bool
	starts  map[uuid.UUID]map[uuid.UUID]int64

	messages map[uuid.UUID][]domain.Message
	nextSeq  map[uuid.UUID]int64

	directRooms map[[2]uuid.UUID]uuid.UUID

	duplicateFor string

	sends int

	beforeList func()
}

func newFakeRooms() *fakeRooms {
	return &fakeRooms{
		members:     make(map[uuid.UUID]map[uuid.UUID]bool),
		starts:      make(map[uuid.UUID]map[uuid.UUID]int64),
		messages:    make(map[uuid.UUID][]domain.Message),
		nextSeq:     make(map[uuid.UUID]int64),
		directRooms: make(map[[2]uuid.UUID]uuid.UUID),
	}
}

func (f *fakeRooms) join(roomID, userID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.members[roomID] == nil {
		f.members[roomID] = make(map[uuid.UUID]bool)
	}
	f.members[roomID][userID] = true
}

func (f *fakeRooms) joinLate(roomID, userID uuid.UUID) {
	f.join(roomID, userID)

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.starts[roomID] == nil {
		f.starts[roomID] = make(map[uuid.UUID]int64)
	}
	f.starts[roomID][userID] = f.nextSeq[roomID]
}

func (f *fakeRooms) HistoryStart(_ context.Context, roomID, userID uuid.UUID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.members[roomID][userID] {
		return 0, domain.ErrNotAMember
	}
	return f.starts[roomID][userID], nil
}

func (f *fakeRooms) Send(_ context.Context, in service.SendInput) (domain.Message, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sends++

	if !f.members[in.RoomID][in.AuthorID] {
		return domain.Message{}, false, domain.ErrNotAMember
	}

	return f.appendLocked(in.RoomID, in.AuthorID, in.Body, in.ClientID)
}

func (f *fakeRooms) SendDirect(
	_ context.Context,
	in service.SendDirectInput,
) (domain.Message, domain.Room, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sends++

	if in.FromUserID == in.ToUserID {
		return domain.Message{}, domain.Room{}, false, domain.ErrCannotMessageSelf
	}

	min, max := domain.DirectPair(in.FromUserID, in.ToUserID)
	pairKey := [2]uuid.UUID{min, max}

	roomID, ok := f.directRooms[pairKey]
	if !ok {
		roomID = uuid.New()
		f.directRooms[pairKey] = roomID
		f.members[roomID] = map[uuid.UUID]bool{in.FromUserID: true, in.ToUserID: true}
	}

	m, duplicate, err := f.appendLocked(roomID, in.FromUserID, in.Body, in.ClientID)
	if err != nil {
		return domain.Message{}, domain.Room{}, false, err
	}

	room := domain.Room{ID: roomID, Kind: domain.RoomKindDirect, DirectUserMin: &min, DirectUserMax: &max}

	return m, room, duplicate, nil
}

func (f *fakeRooms) appendLocked(roomID, authorID uuid.UUID, rawBody, clientID string) (domain.Message, bool, error) {
	body, err := domain.ValidateMessageBody(rawBody)
	if err != nil {
		return domain.Message{}, false, err
	}

	if clientID != "" && clientID == f.duplicateFor {
		for _, m := range f.messages[roomID] {
			if m.ClientID == clientID {
				return m, true, nil
			}
		}
	}

	f.nextSeq[roomID]++
	m := domain.Message{
		ID:       uuid.New(),
		RoomID:   roomID,
		AuthorID: authorID,
		Body:     body,
		ClientID: clientID,
		Kind:     domain.MessageKindText,
		Seq:      f.nextSeq[roomID],
		SentAt:   time.Now().UTC(),
	}
	f.messages[roomID] = append(f.messages[roomID], m)

	return m, false, nil
}

func (f *fakeRooms) ListMessages(
	_ context.Context,
	roomID, userID uuid.UUID,
	page domain.Page,
) (service.History, error) {
	if f.beforeList != nil {
		f.beforeList()
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.members[roomID][userID] {
		return service.History{}, domain.ErrNotAMember
	}

	var all []domain.Message
	for _, m := range f.messages[roomID] {
		if m.Seq > f.starts[roomID][userID] {
			all = append(all, m)
		}
	}
	if page.AfterSeq > 0 {
		var after []domain.Message
		for _, m := range all {
			if m.Seq > page.AfterSeq {
				after = append(after, m)
			}
		}
		return service.History{Messages: after}, nil
	}

	if page.Limit > 0 && len(all) > page.Limit {
		return service.History{Messages: all[len(all)-page.Limit:], HasMore: true}, nil
	}
	return service.History{Messages: all}, nil
}

func (f *fakeRooms) EditMessage(_ context.Context, in service.EditInput) (domain.Message, error) {
	return f.change(in.MessageID, in.UserID, func(m *domain.Message) error {
		body, err := domain.ValidateMessageBody(in.Body)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		m.Body, m.EditedAt = body, &now
		return nil
	})
}

func (f *fakeRooms) DeleteMessage(_ context.Context, messageID, userID uuid.UUID) (domain.Message, error) {
	return f.change(messageID, userID, func(m *domain.Message) error {
		now := time.Now().UTC()
		m.Body, m.DeletedAt = "", &now
		return nil
	})
}

func (f *fakeRooms) change(messageID, userID uuid.UUID, apply func(*domain.Message) error) (domain.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for roomID, messages := range f.messages {
		for i, m := range messages {
			if m.ID != messageID {
				continue
			}
			if !f.members[roomID][userID] {
				return domain.Message{}, domain.ErrMessageNotFound
			}
			if m.AuthorID != userID {
				return domain.Message{}, domain.ErrNotMessageAuthor
			}
			if m.DeletedAt != nil {
				return domain.Message{}, domain.ErrMessageDeleted
			}
			if err := apply(&m); err != nil {
				return domain.Message{}, err
			}
			f.messages[roomID][i] = m
			return m, nil
		}
	}
	return domain.Message{}, domain.ErrMessageNotFound
}

type fakeNames struct{}

func (fakeNames) DisplayName(context.Context, string) string { return "" }

type fakePresence struct {
	mu       sync.Mutex
	watchers map[string][]chan struct{}
	watched  chan string
}

func newFakePresence() *fakePresence {
	return &fakePresence{
		watchers: make(map[string][]chan struct{}),
		watched:  make(chan string, 16),
	}
}

func (f *fakePresence) SubscribeChanged(_ context.Context, userID string) (<-chan struct{}, func(), error) {
	ch := make(chan struct{}, 1)

	f.mu.Lock()
	f.watchers[userID] = append(f.watchers[userID], ch)
	f.mu.Unlock()
	f.watched <- userID

	var once sync.Once
	return ch, func() { once.Do(func() { close(ch) }) }, nil
}

func (f *fakePresence) change(userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, ch := range f.watchers[userID] {
		ch <- struct{}{}
	}
}

func (f *fakePresence) waitWatched(t *testing.T, userID string) {
	t.Helper()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-f.watched:
			if got == userID {
				return
			}
		case <-deadline:
			t.Fatalf("%s was never watched", userID)
		}
	}
}

type harness struct {
	server   *httptest.Server
	rooms    *fakeRooms
	bus      pubsub.Bus
	signals  pubsub.SignalBus
	users    pubsub.UserSignalBus
	presence *fakePresence
	issue    func(t *testing.T, userID uuid.UUID, ttl time.Duration) string
}

type updatesDown struct {
	*pubsub.Memory
}

func (updatesDown) PublishUpdate(context.Context, domain.Message) error {
	return errors.New("the bus is down")
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWithBus(t, func(m *pubsub.Memory) pubsub.Bus { return m })
}

func newHarnessWithBus(t *testing.T, busOf func(*pubsub.Memory) pubsub.Bus) *harness {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	const (
		keyID    = "test-key-1"
		issuer   = "https://auth.axon.test"
		audience = "axon"
	)

	verifier, err := authn.NewVerifier(authn.Config{
		Keys:     staticKeys{id: keyID, key: &key.PublicKey},
		Issuer:   issuer,
		Audience: audience,
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	rooms := newFakeRooms()
	bus := pubsub.NewMemory()
	presence := newFakePresence()

	handler, err := ws.New(ws.Config{
		Service:     rooms,
		Verifier:    verifier,
		Bus:         busOf(bus),
		Signals:     bus,
		UserSignals: bus,
		Names:       fakeNames{},
		Presence:    presence,
		Logger:      logger.Discard(),
	})
	if err != nil {
		t.Fatalf("ws.New: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &harness{
		server:   server,
		rooms:    rooms,
		bus:      bus,
		signals:  bus,
		users:    bus,
		presence: presence,
		issue: func(t *testing.T, userID uuid.UUID, ttl time.Duration) string {
			t.Helper()
			return signToken(t, key, keyID, issuer, audience, userID, ttl)
		},
	}
}

type client struct {
	conn *sharedws.Conn
	t    *testing.T
}

func (h *harness) connect(t *testing.T, userID uuid.UUID) *client {
	t.Helper()
	return h.connectFor(t, userID, time.Hour)
}

func (h *harness) connectFor(t *testing.T, userID uuid.UUID, ttl time.Duration) *client {
	t.Helper()

	header := http.Header{}
	header.Set("Authorization", "Bearer "+h.issue(t, userID, ttl))

	conn, err := sharedws.Dial(t.Context(), wsURL(h.server.URL), sharedws.DialOptions{
		Subprotocols: []string{ws.Subprotocol},
		Header:       header,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	return &client{conn: conn, t: t}
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + ws.Path
}

func (c *client) send(frame ws.Inbound) {
	c.t.Helper()

	payload, err := json.Marshal(frame)
	if err != nil {
		c.t.Fatalf("encode frame: %v", err)
	}
	if err := c.conn.Write(c.t.Context(), payload); err != nil {
		c.t.Fatalf("write %s: %v", frame.Type, err)
	}
}

func (c *client) read() ws.Outbound {
	c.t.Helper()

	ctx, cancel := context.WithTimeout(c.t.Context(), 5*time.Second)
	defer cancel()

	payload, err := c.conn.Read(ctx)
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}

	var out ws.Outbound
	if err := json.Unmarshal(payload, &out); err != nil {
		c.t.Fatalf("decode frame: %v", err)
	}
	return out
}

func (c *client) expect(want string) ws.Outbound {
	c.t.Helper()

	out := c.read()
	if out.Type != want {
		c.t.Fatalf("got a %s frame (%+v), want %s", out.Type, out, want)
	}
	return out
}

func TestASocketWithoutATokenIsRefused(t *testing.T) {
	h := newHarness(t)

	_, err := sharedws.Dial(t.Context(), wsURL(h.server.URL), sharedws.DialOptions{
		Subprotocols: []string{ws.Subprotocol},
	})
	if err == nil {
		t.Fatal("a socket opened with no token")
	}
}

func TestASocketWithAForgedTokenIsRefused(t *testing.T) {
	h := newHarness(t)

	header := http.Header{}
	header.Set("Authorization", "Bearer not-a-token")

	_, err := sharedws.Dial(t.Context(), wsURL(h.server.URL), sharedws.DialOptions{
		Subprotocols: []string{ws.Subprotocol},
		Header:       header,
	})
	if err == nil {
		t.Fatal("a socket opened with a forged token")
	}
}

func TestAMessageReachesTheOtherPersonInTheRoom(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	h.rooms.join(roomID, alice)
	h.rooms.join(roomID, bob)

	ac := h.connect(t, alice)
	bc := h.connect(t, bob)

	ac.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	ac.expect(ws.TypeSubscribed)
	bc.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	bc.expect(ws.TypeSubscribed)

	ac.send(ws.Inbound{
		Type: ws.TypeSend, RoomID: roomID.String(), ClientID: "alice-1", Body: "hello bob",
	})

	ack := ac.expect(ws.TypeAck)
	if ack.ClientID != "alice-1" {
		t.Errorf("ack carries client id %q, want alice-1", ack.ClientID)
	}
	if ack.Seq != 1 {
		t.Errorf("ack reports position %d, want 1", ack.Seq)
	}

	delivered := bc.expect(ws.TypeMessage)
	if delivered.Message.Body != "hello bob" {
		t.Errorf("bob received %q", delivered.Message.Body)
	}
	if delivered.Message.AuthorID != alice.String() {
		t.Errorf("bob received a message attributed to %s, want %s",
			delivered.Message.AuthorID, alice)
	}

	if delivered.Message.ClientID != "" {
		t.Errorf("bob received alice's client id %q", delivered.Message.ClientID)
	}

	own := ac.expect(ws.TypeMessage)
	if own.Message.ClientID != "alice-1" {
		t.Errorf("the sender's own copy carries client id %q, want alice-1", own.Message.ClientID)
	}
}

func TestAStrangerCannotSubscribeOrSend(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	h.rooms.join(roomID, uuid.New())

	stranger := h.connect(t, uuid.New())

	stranger.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	if refusal := stranger.expect(ws.TypeError); refusal.Code != ws.ErrorNotAMember {
		t.Errorf("subscribe was refused with %q, want %q", refusal.Code, ws.ErrorNotAMember)
	}

	stranger.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "let me in"})
	if refusal := stranger.expect(ws.TypeError); refusal.Code != ws.ErrorNotAMember {
		t.Errorf("send was refused with %q, want %q", refusal.Code, ws.ErrorNotAMember)
	}

	stranger.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: uuid.New().String()})
	if refusal := stranger.expect(ws.TypeError); refusal.Code != ws.ErrorNotAMember {
		t.Errorf("a missing room answered %q, want %q", refusal.Code, ws.ErrorNotAMember)
	}
}

func TestAnInvalidFrameIsRefusedWithoutClosingTheSocket(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	user := uuid.New()
	h.rooms.join(roomID, user)

	c := h.connect(t, user)

	c.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: "not-a-uuid"})
	if refusal := c.expect(ws.TypeError); refusal.Code != ws.ErrorInvalid {
		t.Errorf("code = %q, want %q", refusal.Code, ws.ErrorInvalid)
	}

	c.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "   "})
	if refusal := c.expect(ws.TypeError); refusal.Code != ws.ErrorInvalid {
		t.Errorf("code = %q, want %q", refusal.Code, ws.ErrorInvalid)
	}

	c.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	c.expect(ws.TypeSubscribed)
}

func TestAnUnknownFrameTypeClosesTheSocket(t *testing.T) {
	h := newHarness(t)

	c := h.connect(t, uuid.New())
	c.send(ws.Inbound{Type: "sing"})

	if _, err := c.conn.Read(t.Context()); err == nil {
		t.Fatal("the socket stayed open after an undefined frame")
	}
}

func TestSubscribingWithAPositionCatchesUp(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	h.rooms.join(roomID, alice)
	h.rooms.join(roomID, bob)

	ac := h.connect(t, alice)
	ac.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	ac.expect(ws.TypeSubscribed)

	for _, body := range []string{"one", "two", "three"} {
		ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: body})
		ac.expect(ws.TypeAck)
		ac.expect(ws.TypeMessage)
	}

	bc := h.connect(t, bob)
	bc.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String(), Since: 1})

	subscribed := bc.expect(ws.TypeSubscribed)
	if subscribed.Seq != 3 {
		t.Errorf("the room reports position %d, want 3", subscribed.Seq)
	}

	for _, want := range []string{"two", "three"} {
		got := bc.expect(ws.TypeMessage)
		if got.Message.Body != want {
			t.Fatalf("caught up with %q, want %q", got.Message.Body, want)
		}
	}

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "four"})
	ac.expect(ws.TypeAck)

	if got := bc.expect(ws.TypeMessage); got.Message.Body != "four" {
		t.Errorf("live delivery produced %q, want four", got.Message.Body)
	}
}

func TestAResendIsAcknowledgedButNotDeliveredAgain(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	h.rooms.join(roomID, alice)
	h.rooms.join(roomID, bob)
	h.rooms.duplicateFor = "alice-1"

	ac := h.connect(t, alice)
	bc := h.connect(t, bob)

	bc.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	bc.expect(ws.TypeSubscribed)

	frame := ws.Inbound{
		Type: ws.TypeSend, RoomID: roomID.String(), ClientID: "alice-1", Body: "hello",
	}
	ac.send(frame)
	ac.expect(ws.TypeAck)

	if got := bc.expect(ws.TypeMessage); got.Message.Body != "hello" {
		t.Fatalf("bob received %q", got.Message.Body)
	}

	ac.send(frame)
	if ack := ac.expect(ws.TypeAck); ack.Seq != 1 {
		t.Errorf("the resend was acknowledged at position %d, want the original 1", ack.Seq)
	}

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "second"})
	ac.expect(ws.TypeAck)

	if got := bc.expect(ws.TypeMessage); got.Message.Body != "second" {
		t.Errorf("bob's next frame was %q, want second — the resend was delivered again",
			got.Message.Body)
	}
}

func TestUnsubscribingStopsDelivery(t *testing.T) {
	h := newHarness(t)

	roomID, otherID := uuid.New(), uuid.New()
	alice, bob := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{roomID, otherID} {
		h.rooms.join(id, alice)
		h.rooms.join(id, bob)
	}

	ac := h.connect(t, alice)
	bc := h.connect(t, bob)

	for _, id := range []uuid.UUID{roomID, otherID} {
		bc.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: id.String()})
		bc.expect(ws.TypeSubscribed)
	}

	bc.send(ws.Inbound{Type: ws.TypeUnsubscribe, RoomID: roomID.String()})
	bc.send(ws.Inbound{Type: ws.TypeTyping, RoomID: roomID.String()})
	if refusal := bc.expect(ws.TypeError); refusal.Code != ws.ErrorNotAMember {
		t.Fatalf("typing after unsubscribing was answered with %q, want %q", refusal.Code, ws.ErrorNotAMember)
	}

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "unheard"})
	ac.expect(ws.TypeAck)

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: otherID.String(), Body: "heard"})
	ac.expect(ws.TypeAck)

	if got := bc.expect(ws.TypeMessage); got.Message.Body != "heard" {
		t.Errorf("received %q after unsubscribing from its room", got.Message.Body)
	}
}

func TestTheSocketClosesWhenTheTokenExpires(t *testing.T) {
	h := newHarness(t)

	c := h.connectFor(t, uuid.New(), 300*time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if _, err := c.conn.Read(ctx); err == nil {
		t.Fatal("the socket stayed open past the token's expiry")
	} else if code := sharedws.CloseStatus(err); code != ws.CloseTokenExpired {
		t.Errorf("closed with %d, want %d", code, ws.CloseTokenExpired)
	}
}

func TestSubscribingTwiceDeliversOnce(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	h.rooms.join(roomID, alice)
	h.rooms.join(roomID, bob)

	ac := h.connect(t, alice)
	bc := h.connect(t, bob)

	bc.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	bc.expect(ws.TypeSubscribed)
	bc.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "once"})
	ac.expect(ws.TypeAck)

	if got := bc.expect(ws.TypeMessage); got.Message.Body != "once" {
		t.Fatalf("received %q", got.Message.Body)
	}

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "twice"})
	ac.expect(ws.TypeAck)

	if got := bc.expect(ws.TypeMessage); got.Message.Body != "twice" {
		t.Errorf("the next frame was %q, want twice — the room was subscribed twice",
			got.Message.Body)
	}
}

func TestAReadReceiptReachesTheOtherPersonButNotItsAuthor(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	h.rooms.join(roomID, alice)
	h.rooms.join(roomID, bob)

	ac := h.connect(t, alice)
	bc := h.connect(t, bob)

	ac.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	ac.expect(ws.TypeSubscribed)
	bc.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	bc.expect(ws.TypeSubscribed)

	if err := h.signals.PublishSignal(t.Context(), pubsub.Signal{
		Kind: pubsub.SignalRead, RoomID: roomID, UserID: bob, Seq: 3,
	}); err != nil {
		t.Fatalf("publish read receipt: %v", err)
	}

	got := ac.expect(ws.TypeRead)
	if got.UserID != bob.String() {
		t.Errorf("read receipt reports user %q, want %s", got.UserID, bob)
	}
	if got.RoomID != roomID.String() {
		t.Errorf("read receipt reports room %q, want %s", got.RoomID, roomID)
	}
	if got.Seq != 3 {
		t.Errorf("read receipt reports seq %d, want 3", got.Seq)
	}

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "so alice knows the socket is still alive"})
	ac.expect(ws.TypeAck)
	ac.expect(ws.TypeMessage)
	bc.expect(ws.TypeMessage)

	if err := h.signals.PublishSignal(t.Context(), pubsub.Signal{
		Kind: pubsub.SignalRead, RoomID: roomID, UserID: bob, Seq: 4,
	}); err != nil {
		t.Fatalf("publish read receipt: %v", err)
	}
	ac.expect(ws.TypeRead)
}

func TestTypingReachesTheOtherPersonButNotTheTypist(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	h.rooms.join(roomID, alice)
	h.rooms.join(roomID, bob)

	ac := h.connect(t, alice)
	bc := h.connect(t, bob)

	ac.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	ac.expect(ws.TypeSubscribed)
	bc.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	bc.expect(ws.TypeSubscribed)

	ac.send(ws.Inbound{Type: ws.TypeTyping, RoomID: roomID.String()})

	got := bc.expect(ws.TypeTyping)
	if got.UserID != alice.String() || got.RoomID != roomID.String() {
		t.Errorf("bob saw %s typing in %s, want %s in %s", got.UserID, got.RoomID, alice, roomID)
	}

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "done typing"})
	ac.expect(ws.TypeAck)
}

func TestTypingInARoomYouHaveNotOpenedIsRefused(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice := uuid.New()
	h.rooms.join(roomID, alice)

	ac := h.connect(t, alice)
	ac.send(ws.Inbound{Type: ws.TypeTyping, RoomID: roomID.String()})

	if refusal := ac.expect(ws.TypeError); refusal.Code != ws.ErrorNotAMember {
		t.Errorf("code = %q, want %q", refusal.Code, ws.ErrorNotAMember)
	}
}

func TestAFirstDirectMessageAnnouncesTheNewRoomToBothPeople(t *testing.T) {
	h := newHarness(t)

	alice, bob := uuid.New(), uuid.New()

	ac := h.connect(t, alice)
	bc := h.connect(t, bob)
	otherTab := h.connect(t, alice)

	ac.send(ws.Inbound{Type: ws.TypeSend, ToUserID: bob.String(), ClientID: "c1", Body: "hi bob"})
	ack := ac.expect(ws.TypeAck)

	for name, c := range map[string]*client{"bob": bc, "alice's other tab": otherTab} {
		got := c.expect(ws.TypeRoomAdded)
		if got.RoomID != ack.RoomID {
			t.Errorf("%s was told about room %q, want %q", name, got.RoomID, ack.RoomID)
		}
	}

	if got := ac.expect(ws.TypeRoomAdded); got.RoomID != ack.RoomID {
		t.Errorf("the sending tab was told about room %q, want %q", got.RoomID, ack.RoomID)
	}
}

func TestAWatchedPersonsPresenceChangeIsPushed(t *testing.T) {
	h := newHarness(t)

	alice, bob := uuid.New(), uuid.New()
	ac := h.connect(t, alice)

	ac.send(ws.Inbound{Type: ws.TypeWatchPresence, ToUserID: bob.String()})
	h.presence.waitWatched(t, bob.String())

	h.presence.change(bob.String())

	if got := ac.expect(ws.TypePresence); got.UserID != bob.String() {
		t.Errorf("presence frame for %q, want %s", got.UserID, bob)
	}
}

func TestAnEditReachesEveryoneInTheRoom(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	h.rooms.join(roomID, alice)
	h.rooms.join(roomID, bob)

	ac := h.connect(t, alice)
	bc := h.connect(t, bob)
	for _, c := range []*client{ac, bc} {
		c.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
		c.expect(ws.TypeSubscribed)
	}

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "helo"})
	ac.expect(ws.TypeAck)
	ac.expect(ws.TypeMessage)
	sent := bc.expect(ws.TypeMessage)

	ac.send(ws.Inbound{Type: ws.TypeEdit, MessageID: sent.Message.ID, Body: "hello"})

	got := bc.expect(ws.TypeMessageUpdated)
	if got.Message.ID != sent.Message.ID || got.Message.Body != "hello" || got.Message.EditedAt == nil {
		t.Errorf("bob saw %+v, want the same message edited to hello", got.Message)
	}
	if own := ac.expect(ws.TypeMessageUpdated); own.Message.Body != "hello" {
		t.Errorf("alice saw %q, want hello", own.Message.Body)
	}

	ac.send(ws.Inbound{Type: ws.TypeDelete, MessageID: sent.Message.ID})
	if gone := bc.expect(ws.TypeMessageUpdated); gone.Message.DeletedAt == nil || gone.Message.Body != "" {
		t.Errorf("bob saw %+v, want the message deleted", gone.Message)
	}
}

func TestAnEditFromBeforeYouJoinedIsNotPushedToYou(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice, carol := uuid.New(), uuid.New()
	h.rooms.join(roomID, alice)

	ac := h.connect(t, alice)
	ac.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	ac.expect(ws.TypeSubscribed)
	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "before carol"})
	ac.expect(ws.TypeAck)
	old := ac.expect(ws.TypeMessage)

	h.rooms.joinLate(roomID, carol)
	cc := h.connect(t, carol)
	cc.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	cc.expect(ws.TypeSubscribed)

	ac.send(ws.Inbound{Type: ws.TypeEdit, MessageID: old.Message.ID, Body: "still before carol"})
	ac.expect(ws.TypeMessageUpdated)

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "welcome"})
	ac.expect(ws.TypeAck)

	if got := cc.expect(ws.TypeMessage); got.Message.Body != "welcome" {
		t.Errorf("carol's next frame carried %q, want welcome", got.Message.Body)
	}
}

func TestTheEditorHearsBackWhenTheBusIsDown(t *testing.T) {
	h := newHarnessWithBus(t, func(m *pubsub.Memory) pubsub.Bus { return updatesDown{m} })

	roomID := uuid.New()
	alice := uuid.New()
	h.rooms.join(roomID, alice)

	ac := h.connect(t, alice)
	ac.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	ac.expect(ws.TypeSubscribed)
	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "helo"})
	ac.expect(ws.TypeAck)
	sent := ac.expect(ws.TypeMessage)

	ac.send(ws.Inbound{Type: ws.TypeEdit, MessageID: sent.Message.ID, Body: "hello"})
	if got := ac.expect(ws.TypeMessageUpdated); got.Message.Body != "hello" {
		t.Errorf("the editor heard %q, want hello", got.Message.Body)
	}
}

func TestSomeoneElsesMessageCannotBeChanged(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	h.rooms.join(roomID, alice)
	h.rooms.join(roomID, bob)

	ac := h.connect(t, alice)
	bc := h.connect(t, bob)

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "mine"})
	ack := ac.expect(ws.TypeAck)
	messageID := h.rooms.messages[uuid.MustParse(ack.RoomID)][0].ID

	bc.send(ws.Inbound{Type: ws.TypeEdit, MessageID: messageID.String(), Body: "yours"})
	if refusal := bc.expect(ws.TypeError); refusal.Code != ws.ErrorForbidden {
		t.Errorf("edit was refused with %q, want %q", refusal.Code, ws.ErrorForbidden)
	}

	bc.send(ws.Inbound{Type: ws.TypeDelete, MessageID: "nope"})
	if refusal := bc.expect(ws.TypeError); refusal.Code != ws.ErrorInvalid {
		t.Errorf("a bad id was refused with %q, want %q", refusal.Code, ws.ErrorInvalid)
	}
}

func TestLeavingARoomStopsItsDelivery(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	h.rooms.join(roomID, alice)
	h.rooms.join(roomID, bob)

	ac := h.connect(t, alice)
	bc := h.connect(t, bob)
	bc.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	bc.expect(ws.TypeSubscribed)

	if err := h.users.PublishUserSignal(t.Context(), bob, pubsub.Signal{
		Kind: pubsub.SignalRoomRemoved, RoomID: roomID, UserID: bob,
	}); err != nil {
		t.Fatalf("publish room removed: %v", err)
	}
	if got := bc.expect(ws.TypeRoomRemoved); got.RoomID != roomID.String() {
		t.Errorf("room_removed for %q, want %s", got.RoomID, roomID)
	}

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "unheard"})
	ac.expect(ws.TypeAck)

	bc.send(ws.Inbound{Type: ws.TypeTyping, RoomID: roomID.String()})
	if refusal := bc.expect(ws.TypeError); refusal.Code != ws.ErrorNotAMember {
		t.Errorf("the next frame was %+v, want the typing refusal — the room was still delivered", refusal)
	}
}

func TestRemovalDuringCatchUpStillStopsDelivery(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	h.rooms.join(roomID, alice)
	h.rooms.join(roomID, bob)

	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h.rooms.beforeList = func() {
		once.Do(func() {
			close(entered)
			<-release
		})
	}

	ac := h.connect(t, alice)
	bc := h.connect(t, bob)
	bc.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	<-entered

	if err := h.users.PublishUserSignal(t.Context(), bob, pubsub.Signal{
		Kind: pubsub.SignalRoomRemoved, RoomID: roomID, UserID: bob,
	}); err != nil {
		t.Fatalf("publish room removed: %v", err)
	}
	bc.expect(ws.TypeRoomRemoved)
	close(release)
	bc.expect(ws.TypeSubscribed)

	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "unheard"})
	ac.expect(ws.TypeAck)

	bc.send(ws.Inbound{Type: ws.TypeTyping, RoomID: roomID.String()})
	if refusal := bc.expect(ws.TypeError); refusal.Code != ws.ErrorNotAMember {
		t.Errorf("the next frame was %+v, want the typing refusal — the room was still delivered", refusal)
	}
}

type staticKeys struct {
	id  string
	key *rsa.PublicKey
}

func (s staticKeys) PublicKey(keyID string) (*rsa.PublicKey, bool) {
	if keyID != s.id {
		return nil, false
	}
	return s.key, true
}

func signToken(
	t *testing.T,
	key *rsa.PrivateKey,
	keyID, issuer, audience string,
	userID uuid.UUID,
	ttl time.Duration,
) string {
	t.Helper()

	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": userID.String(),
		"iss": issuer,
		"aud": audience,
		"jti": uuid.NewString(),
		"iat": now.Unix(),
		"exp": now.Add(ttl).Unix(),
	})
	token.Header["kid"] = keyID

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}
