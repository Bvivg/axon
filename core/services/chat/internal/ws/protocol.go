package ws

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/ws"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const Subprotocol = "axon.chat.v1"

const (
	TypeSubscribe = "subscribe"

	TypeUnsubscribe = "unsubscribe"

	TypeSend = "send"
)

const (
	TypeMessage = "message"

	TypeSubscribed = "subscribed"

	TypeAck = "ack"

	TypeError = "error"
)

const (
	CloseUnauthenticated ws.StatusCode = 4401

	CloseTokenExpired ws.StatusCode = 4402

	CloseProtocol ws.StatusCode = 4400
)

const (
	ErrorNotAMember = "not_a_member"

	ErrorInvalid = "invalid"

	ErrorInternal = "internal"
)

type Inbound struct {
	Type   string `json:"type"`
	RoomID string `json:"room_id"`

	Since int64 `json:"since,omitempty"`

	ClientID string `json:"client_id,omitempty"`

	Body string `json:"body,omitempty"`
}

type Outbound struct {
	Type string `json:"type"`

	Message *Message `json:"message,omitempty"`

	RoomID string `json:"room_id,omitempty"`
	Seq    int64  `json:"seq,omitempty"`

	ClientID string `json:"client_id,omitempty"`
	Code     string `json:"code,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type Message struct {
	ID       string    `json:"id"`
	RoomID   string    `json:"room_id"`
	AuthorID string    `json:"author_id"`
	Body     string    `json:"body"`
	Seq      int64     `json:"seq"`
	SentAt   time.Time `json:"sent_at"`
	ClientID string    `json:"client_id,omitempty"`
}

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

func encode(out Outbound) ([]byte, error) {
	payload, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("ws: encode %s frame: %w", out.Type, err)
	}
	return payload, nil
}
