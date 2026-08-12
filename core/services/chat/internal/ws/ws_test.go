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

// fakeRooms stands in for the chat service. What is under test here is the
// protocol, so this answers in whatever way a scenario needs and records what
// it was asked.
type fakeRooms struct {
	mu sync.Mutex

	// members is who belongs where. Anything not listed answers as it would for
	// a room that does not exist, which is the same answer.
	members map[uuid.UUID]map[uuid.UUID]bool

	messages map[uuid.UUID][]domain.Message
	nextSeq  map[uuid.UUID]int64

	// duplicateFor makes Send report a resend for a client id, as it would for
	// a message that was already written.
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

// harness is a running socket server and the pieces behind it.
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

// client is one connected socket, with the frames it has received.
type client struct {
	conn *sharedws.Conn
	t    *testing.T
}

// connect opens a socket as the given user.
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

// read waits for the next frame.
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

// expect waits for a frame of the given type, failing on anything else.
func (c *client) expect(want string) ws.Outbound {
	c.t.Helper()

	out := c.read()
	if out.Type != want {
		c.t.Fatalf("got a %s frame (%+v), want %s", out.Type, out, want)
	}
	return out
}

// A socket is a credential-bearing thing like any other request, and the token
// is checked before the upgrade so a client without one gets an answer it can
// read rather than a connection that opens and vanishes.
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

// The whole point of the thing: what one person says appears in front of the
// other without either of them asking again.
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

	// The sender is acknowledged first: that is the point at which the message
	// is durable, and it is what tells a client it need not resend.
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

	// One client's id for a message is its own bookkeeping. Echoing it to
	// everybody else would hand out ids that belong to somebody else's client.
	if delivered.Message.ClientID != "" {
		t.Errorf("bob received alice's client id %q", delivered.Message.ClientID)
	}

	// And the sender sees their own message come back through the room, with
	// their own id on it, so an optimistic draw can be reconciled.
	own := ac.expect(ws.TypeMessage)
	if own.Message.ClientID != "alice-1" {
		t.Errorf("the sender's own copy carries client id %q, want alice-1", own.Message.ClientID)
	}
}

// A room somebody does not belong to answers the same way a room that does not
// exist does, and neither ends the connection: one bad room id is not a reason
// to drop every other conversation on the socket.
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

	// A room that genuinely does not exist is indistinguishable.
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

	// Still usable afterwards.
	c.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	c.expect(ws.TypeSubscribed)
}

// A frame this protocol does not define is not a mistake to answer politely: it
// means the peer is speaking something else.
func TestAnUnknownFrameTypeClosesTheSocket(t *testing.T) {
	h := newHarness(t)

	c := h.connect(t, uuid.New())
	c.send(ws.Inbound{Type: "sing"})

	if _, err := c.conn.Read(t.Context()); err == nil {
		t.Fatal("the socket stayed open after an undefined frame")
	}
}

// The reconnect story: a client that says where it left off is given what it
// missed, in order, before live delivery starts — and nothing twice.
func TestSubscribingWithAPositionCatchesUp(t *testing.T) {
	h := newHarness(t)

	roomID := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	h.rooms.join(roomID, alice)
	h.rooms.join(roomID, bob)

	// Said while bob was away.
	ac := h.connect(t, alice)
	ac.send(ws.Inbound{Type: ws.TypeSubscribe, RoomID: roomID.String()})
	ac.expect(ws.TypeSubscribed)

	for _, body := range []string{"one", "two", "three"} {
		ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: body})
		ac.expect(ws.TypeAck)
		ac.expect(ws.TypeMessage)
	}

	// Bob comes back holding position 1.
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

	// And live delivery continues from there, with no repeat of the catch-up.
	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "four"})
	ac.expect(ws.TypeAck)

	if got := bc.expect(ws.TypeMessage); got.Message.Body != "four" {
		t.Errorf("live delivery produced %q, want four", got.Message.Body)
	}
}

// A resend is answered, not repeated: the sender gets its acknowledgement, and
// the room is not shown the message a second time.
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

	// The same message again, as a client would after losing its connection
	// between sending and being acknowledged.
	ac.send(frame)
	if ack := ac.expect(ws.TypeAck); ack.Seq != 1 {
		t.Errorf("the resend was acknowledged at position %d, want the original 1", ack.Seq)
	}

	// Bob must not see it twice. Something else has to arrive for that to be
	// observable rather than merely not yet observed.
	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "second"})
	ac.expect(ws.TypeAck)

	if got := bc.expect(ws.TypeMessage); got.Message.Body != "second" {
		t.Errorf("bob's next frame was %q, want second — the resend was delivered again",
			got.Message.Body)
	}
}

// Unsubscribing stops delivery without ending the connection.
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

	// The first message must not arrive; the second must. Reading once is what
	// distinguishes the two: if the unsubscribe did nothing, this frame is the
	// one it was supposed to stop.
	if got := bc.expect(ws.TypeMessage); got.Message.Body != "heard" {
		t.Errorf("received %q after unsubscribing from its room", got.Message.Body)
	}
}

// A socket may not outlive the token that opened it. Fifteen-minute access
// tokens mean nothing if one connection can hold a dead credential open for a
// day.
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

// Subscribing twice to one room must not open a second delivery: the client
// would then see every message in it twice.
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

	// A second copy would be sitting in front of this one.
	ac.send(ws.Inbound{Type: ws.TypeSend, RoomID: roomID.String(), Body: "twice"})
	ac.expect(ws.TypeAck)

	if got := bc.expect(ws.TypeMessage); got.Message.Body != "twice" {
		t.Errorf("the next frame was %q, want twice — the room was subscribed twice",
			got.Message.Body)
	}
}

// staticKeys is the key set a test signs against: one key, published under one
// id, with no JWKS document and no network in between.
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

// signToken mints an access token of the shape auth issues.
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
