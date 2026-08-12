// Package ws is the chat service's realtime protocol.
//
// Connect carries what a client asks for; this carries what it waits for.
// rules/api-contracts.md keeps the two apart on purpose — a unary call cannot
// push, and a socket that tunnels request-response frames is a worse Connect.
//
// The wire format is JSON rather than protobuf. The contract in shared/proto
// describes rooms and history, and nothing on this socket appears in it: a
// second generated surface for six frame types would cost more than it explains.
// The version lives in the subprotocol name instead, which gives the same room
// to change as a v2 package would.
package ws

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/ws"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

// Subprotocol names this protocol and its version. A client that offers
// something else is refused at the handshake rather than at the first frame it
// sends.
const Subprotocol = "axon.chat.v1"

// Frame types, client to server.
const (
	// TypeSubscribe starts delivery for a room, optionally catching up from a
	// position the client already has.
	TypeSubscribe = "subscribe"
	// TypeUnsubscribe stops it.
	TypeUnsubscribe = "unsubscribe"
	// TypeSend says something.
	TypeSend = "send"
)

// Frame types, server to client.
const (
	// TypeMessage is somebody's message, including the sender's own coming back
	// through the room.
	TypeMessage = "message"
	// TypeSubscribed confirms a subscription and reports where the room is, so
	// a client knows whether it is up to date.
	TypeSubscribed = "subscribed"
	// TypeAck confirms one send, which is the point at which the message is
	// durable.
	TypeAck = "ack"
	// TypeError reports a refusal that does not end the connection.
	TypeError = "error"
)

// Close codes above 4000 are this application's own; the protocol reserves that
// range for exactly this.
const (
	// CloseUnauthenticated means the socket arrived without a usable token.
	CloseUnauthenticated ws.StatusCode = 4401
	// CloseTokenExpired means the token that opened the socket has run out.
	// The connection is closed rather than downgraded: a client with a fresh
	// token reconnects and catches up, and holding the socket open on an
	// expired credential is exactly what short token lifetimes are meant to
	// prevent.
	CloseTokenExpired ws.StatusCode = 4402
	// CloseProtocol means a frame this protocol does not define.
	CloseProtocol ws.StatusCode = 4400
)

// Error codes carried in an error frame. They are stable strings rather than
// numbers because they are read by people as often as by code.
const (
	// ErrorNotAMember covers both a room the caller does not belong to and a
	// room that does not exist. The two are one answer on purpose — see
	// domain.ErrNotAMember.
	ErrorNotAMember = "not_a_member"
	// ErrorInvalid is a frame the server understood and refused: an empty
	// message, one over the length limit, a malformed id.
	ErrorInvalid = "invalid"
	// ErrorInternal is everything else, and carries no detail.
	ErrorInternal = "internal"
)

// Inbound is a frame from a client.
//
// One struct rather than a type per frame: the fields that overlap overlap
// exactly, and the alternative is decoding twice — once to read the type, once
// to read the body — for six small shapes.
type Inbound struct {
	Type   string `json:"type"`
	RoomID string `json:"room_id"`

	// Since is where the client has already caught up to, on a subscribe. Zero
	// means "just start sending"; a client that wants history asks for it over
	// ListMessages, which pages.
	Since int64 `json:"since,omitempty"`

	// ClientID is the sender's own id for a message, echoed back on the ack and
	// on the message itself. It is what makes a resend after a dropped
	// connection idempotent.
	ClientID string `json:"client_id,omitempty"`

	Body string `json:"body,omitempty"`
}

// Outbound is a frame to a client.
type Outbound struct {
	Type string `json:"type"`

	// Message is present on a message frame.
	Message *Message `json:"message,omitempty"`

	// RoomID and Seq are present on a subscribed frame: the room, and the
	// position it has reached, so a client can tell at once whether it has
	// missed anything.
	RoomID string `json:"room_id,omitempty"`
	Seq    int64  `json:"seq,omitempty"`

	// ClientID and Code are present on an ack and an error respectively; both
	// carry the client id when the frame answers a particular send.
	ClientID string `json:"client_id,omitempty"`
	Code     string `json:"code,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Message is a message on the wire.
type Message struct {
	ID       string    `json:"id"`
	RoomID   string    `json:"room_id"`
	AuthorID string    `json:"author_id"`
	Body     string    `json:"body"`
	Seq      int64     `json:"seq"`
	SentAt   time.Time `json:"sent_at"`
	ClientID string    `json:"client_id,omitempty"`
}

// toWire converts a domain message for delivery.
func toWire(m domain.Message) Message {
	return Message{
		ID:       m.ID.String(),
		RoomID:   m.RoomID.String(),
		AuthorID: m.AuthorID.String(),
		Body:     m.Body,
		Seq:      m.Seq,
		SentAt:   m.SentAt,
		ClientID: m.ClientID,
	}
}

// messageFrame wraps a message for delivery.
//
// The client id travels only to the person who sent it. To everybody else it is
// somebody else's bookkeeping, and echoing it would leak one client's internal
// ids to every other client in the room.
func messageFrame(m domain.Message, recipient string) Outbound {
	wire := toWire(m)
	if wire.AuthorID != recipient {
		wire.ClientID = ""
	}
	return Outbound{Type: TypeMessage, Message: &wire}
}

func ackFrame(m domain.Message) Outbound {
	return Outbound{Type: TypeAck, ClientID: m.ClientID, Seq: m.Seq, RoomID: m.RoomID.String()}
}

func subscribedFrame(roomID string, seq int64) Outbound {
	return Outbound{Type: TypeSubscribed, RoomID: roomID, Seq: seq}
}

func errorFrame(code, reason, clientID string) Outbound {
	return Outbound{Type: TypeError, Code: code, Reason: reason, ClientID: clientID}
}

// decode reads one inbound frame.
func decode(payload []byte) (Inbound, error) {
	var in Inbound
	if err := json.Unmarshal(payload, &in); err != nil {
		return Inbound{}, fmt.Errorf("ws: decode frame: %w", err)
	}
	if in.Type == "" {
		return Inbound{}, fmt.Errorf("ws: frame has no type")
	}
	return in, nil
}

// encode writes one outbound frame.
func encode(out Outbound) ([]byte, error) {
	payload, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("ws: encode %s frame: %w", out.Type, err)
	}
	return payload, nil
}
