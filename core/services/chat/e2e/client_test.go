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

var nextCaller atomic.Uint32

type user struct {
	id     string
	email  string
	token  string
	chat   chatv1connect.ChatServiceClient
	dialer *http.Client
}

const testPassword = "correct-horse-battery-staple"

func newUser(t *testing.T) *user {
	t.Helper()

	n := nextCaller.Add(1)

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

type forwardedFor struct{ addr string }

func (f forwardedFor) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-Forwarded-For", f.addr)
	return http.DefaultTransport.RoundTrip(req)
}

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

func (u *user) joinRoom(t *testing.T, roomID string) {
	t.Helper()

	if _, err := u.chat.JoinRoom(context.Background(), connect.NewRequest(&chatv1.JoinRoomRequest{
		RoomId: roomID,
	})); err != nil {
		t.Fatalf("join room: %v", err)
	}
}

type socket struct {
	conn *ws.Conn
	t    *testing.T
}

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

func (s *socket) expect(want string) outbound {
	s.t.Helper()

	out := s.read()
	if out.Type != want {
		s.t.Fatalf("got a %s frame (%+v), want %s", out.Type, out, want)
	}
	return out
}

func (s *socket) subscribe(roomID string, since int64) int64 {
	s.t.Helper()

	s.send(inbound{Type: typeSubscribe, RoomID: roomID, Since: since})
	return s.expect(typeSubscribed).Seq
}
