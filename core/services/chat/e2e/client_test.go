//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

// The frame vocabulary, repeated here rather than imported.
//
// The chat service's ws package is internal, and that is the point: this suite
// is a client, and a client only has what the protocol documents. Importing the
// server's own constants would let a rename pass unnoticed on both sides at
// once.
const (
	subprotocol  = "axon.chat.v1"
	bearerPrefix = "axon.bearer."

	typeSubscribe   = "subscribe"
	typeUnsubscribe = "unsubscribe"
	typeSend        = "send"

	typeMessage    = "message"
	typeSubscribed = "subscribed"
	typeAck        = "ack"
	typeError      = "error"

	errorNotAMember = "not_a_member"
)

type inbound struct {
	Type     string `json:"type"`
	RoomID   string `json:"room_id,omitempty"`
	Since    int64  `json:"since,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	Body     string `json:"body,omitempty"`
}

type outbound struct {
	Type     string `json:"type"`
	RoomID   string `json:"room_id,omitempty"`
	Seq      int64  `json:"seq,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	Code     string `json:"code,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Message  *struct {
		ID       string    `json:"id"`
		RoomID   string    `json:"room_id"`
		AuthorID string    `json:"author_id"`
		Body     string    `json:"body"`
		Seq      int64     `json:"seq"`
		SentAt   time.Time `json:"sent_at"`
		ClientID string    `json:"client_id,omitempty"`
	} `json:"message,omitempty"`
}

// nextCaller hands out a distinct forged address per caller: every request in a
// run leaves the same container, so without it the first scenario to exhaust a
// rate-limit budget would fail all the others.
var nextCaller atomic.Uint32

// user is one signed-in person with clients for both halves of the API.
type user struct {
	id     string
	email  string
	token  string
	chat   chatv1connect.ChatServiceClient
	dialer *http.Client
}

const testPassword = "correct-horse-battery-staple"

// newUser registers an account through the gateway and returns a client for it.
func newUser(t *testing.T) *user {
	t.Helper()

	n := nextCaller.Add(1)
	// 10.0.0.0/8 is private and unroutable: an address that escaped into a log
	// is obviously synthetic.
	addr := fmt.Sprintf("10.%d.%d.%d", (n>>16)&0xff, (n>>8)&0xff, n&0xff)

	httpClient := &http.Client{Transport: forwardedFor{addr: addr}}

	auth := authv1connect.NewAuthServiceClient(httpClient, gatewayURL)

	address := "e2e-" + uuid.NewString() + "@axon.test"
	registered, err := auth.Register(context.Background(), connect.NewRequest(&authv1.RegisterRequest{
		Email:    address,
		Password: testPassword,
	}))
	if err != nil {
		t.Fatalf("register %s: %v", address, err)
	}

	token := registered.Msg.GetTokens().GetAccessToken()

	return &user{
		id:    registered.Msg.GetUser().GetId(),
		email: address,
		token: token,
		chat: chatv1connect.NewChatServiceClient(
			&http.Client{Transport: bearer{token: token, addr: addr}},
			gatewayURL,
		),
		dialer: httpClient,
	}
}

// forwardedFor stamps the caller's address on every request.
type forwardedFor struct{ addr string }

func (f forwardedFor) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-Forwarded-For", f.addr)
	return http.DefaultTransport.RoundTrip(req)
}

// bearer adds the access token as well.
type bearer struct {
	token string
	addr  string
}

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("X-Forwarded-For", b.addr)
	return http.DefaultTransport.RoundTrip(req)
}

// createRoom opens a room through the contract.
func (u *user) createRoom(t *testing.T, name string) *chatv1.Room {
	t.Helper()

	created, err := u.chat.CreateRoom(context.Background(), connect.NewRequest(&chatv1.CreateRoomRequest{
		Name: name,
	}))
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	return created.Msg.GetRoom()
}

// joinRoom puts the user in a room.
func (u *user) joinRoom(t *testing.T, roomID string) {
	t.Helper()

	if _, err := u.chat.JoinRoom(context.Background(), connect.NewRequest(&chatv1.JoinRoomRequest{
		RoomId: roomID,
	})); err != nil {
		t.Fatalf("join room: %v", err)
	}
}

// socket is one open connection to the gateway.
type socket struct {
	conn *ws.Conn
	t    *testing.T
}

// connect opens the chat socket the way a browser does: through the gateway,
// with the token offered as a subprotocol because a page cannot set a header on
// an upgrade.
func (u *user) connect(t *testing.T) *socket {
	t.Helper()

	conn, err := ws.Dial(context.Background(), gatewaySocketURL, ws.DialOptions{
		Subprotocols: []string{subprotocol, bearerPrefix + u.token},
		Options:      ws.Options{Logger: logger.Discard()},
	})
	if err != nil {
		t.Fatalf("open the chat socket: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	return &socket{conn: conn, t: t}
}

func (s *socket) send(frame inbound) {
	s.t.Helper()

	payload, err := json.Marshal(frame)
	if err != nil {
		s.t.Fatalf("encode %s: %v", frame.Type, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.conn.Write(ctx, payload); err != nil {
		s.t.Fatalf("write %s: %v", frame.Type, err)
	}
}

// read waits for the next frame.
func (s *socket) read() outbound {
	s.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	payload, err := s.conn.Read(ctx)
	if err != nil {
		s.t.Fatalf("read: %v", err)
	}

	var out outbound
	if err := json.Unmarshal(payload, &out); err != nil {
		s.t.Fatalf("decode frame: %v", err)
	}
	return out
}

// expect waits for a frame of the given type.
func (s *socket) expect(want string) outbound {
	s.t.Helper()

	out := s.read()
	if out.Type != want {
		s.t.Fatalf("got a %s frame (%+v), want %s", out.Type, out, want)
	}
	return out
}

// subscribe joins the room's delivery and returns where the room has got to.
func (s *socket) subscribe(roomID string, since int64) int64 {
	s.t.Helper()

	s.send(inbound{Type: typeSubscribe, RoomID: roomID, Since: since})
	return s.expect(typeSubscribed).Seq
}
