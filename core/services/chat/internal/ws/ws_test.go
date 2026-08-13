package ws_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
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

	messages map[uuid.UUID][]domain.Message
	nextSeq  map[uuid.UUID]int64

	duplicateFor string

	sends int
}

func newFakeRooms() *fakeRooms {
	return &fakeRooms{
		members:  make(map[uuid.UUID]map[uuid.UUID]bool),
		messages: make(map[uuid.UUID][]domain.Message),
		nextSeq:  make(map[uuid.UUID]int64),
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

func (f *fakeRooms) Send(_ context.Context, in service.SendInput) (domain.Message, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sends++

	if !f.members[in.RoomID][in.AuthorID] {
		return domain.Message{}, false, domain.ErrNotAMember
	}
	body, err := domain.ValidateMessageBody(in.Body)
	if err != nil {
		return domain.Message{}, false, err
	}

	if in.ClientID != "" && in.ClientID == f.duplicateFor {
		for _, m := range f.messages[in.RoomID] {
			if m.ClientID == in.ClientID {
				return m, true, nil
			}
		}
	}

	f.nextSeq[in.RoomID]++
	m := domain.Message{
		ID:       uuid.New(),
		RoomID:   in.RoomID,
		AuthorID: in.AuthorID,
		Body:     body,
		ClientID: in.ClientID,
		Seq:      f.nextSeq[in.RoomID],
		SentAt:   time.Now().UTC(),
	}
	f.messages[in.RoomID] = append(f.messages[in.RoomID], m)

	return m, false, nil
}

func (f *fakeRooms) ListMessages(
	_ context.Context,
	roomID, userID uuid.UUID,
	page domain.Page,
) (service.History, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.members[roomID][userID] {
		return service.History{}, domain.ErrNotAMember
	}

	all := f.messages[roomID]
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

type harness struct {
	server *httptest.Server
	rooms  *fakeRooms
	bus    pubsub.Bus
	issue  func(t *testing.T, userID uuid.UUID, ttl time.Duration) string
}

func newHarness(t *testing.T) *harness {
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

	handler, err := ws.New(ws.Config{
		Service:  rooms,
		Verifier: verifier,
		Bus:      bus,
		Logger:   logger.Discard(),
	})
	if err != nil {
		t.Fatalf("ws.New: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &harness{
		server: server,
		rooms:  rooms,
		bus:    bus,
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
