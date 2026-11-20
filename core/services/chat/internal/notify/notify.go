package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const (
	queueDepth   = 1024
	sendTimeout  = 10 * time.Second
	previewRunes = 140
)

type Store interface {
	RoomByID(ctx context.Context, id uuid.UUID) (domain.Room, error)
	Members(ctx context.Context, roomID uuid.UUID) ([]domain.Member, error)
	PushSubscriptions(ctx context.Context, userIDs []uuid.UUID) ([]domain.PushSubscription, error)
	DropPushEndpoint(ctx context.Context, endpoint string) error
}

type Presence interface {
	Visible(ctx context.Context, userIDs []string) (map[string]bool, error)
}

type Sender interface {
	Send(ctx context.Context, sub domain.PushSubscription, payload []byte, topic string) (int, error)
}

type Config struct {
	Store    Store
	Presence Presence
	Sender   Sender
	Logger   *slog.Logger
}

type Notifier struct {
	store    Store
	presence Presence
	sender   Sender
	log      *slog.Logger
	queue    chan domain.Message
}

func New(cfg Config) (*Notifier, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("notify: a store is required")
	case cfg.Presence == nil:
		return nil, errors.New("notify: presence is required")
	case cfg.Sender == nil:
		return nil, errors.New("notify: a push sender is required")
	case cfg.Logger == nil:
		return nil, errors.New("notify: logger is required")
	}
	return &Notifier{
		store:    cfg.Store,
		presence: cfg.Presence,
		sender:   cfg.Sender,
		log:      cfg.Logger,
		queue:    make(chan domain.Message, queueDepth),
	}, nil
}

func (n *Notifier) MessageSent(_ context.Context, m domain.Message) {
	if m.Kind == domain.MessageKindSystem {
		return
	}
	select {
	case n.queue <- m:
	default:
		n.log.Warn("push queue is full, a notification is dropped", "room_id", m.RoomID, "message_id", m.ID)
	}
}

func (n *Notifier) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-n.queue:
			if err := n.Notify(ctx, m); err != nil {
				n.log.WarnContext(ctx, "could not push a message", "room_id", m.RoomID, "message_id", m.ID, "error", err)
			}
		}
	}
}

type Payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

func (n *Notifier) Notify(ctx context.Context, m domain.Message) error {
	members, err := n.store.Members(ctx, m.RoomID)
	if err != nil {
		return fmt.Errorf("notify: members: %w", err)
	}

	var (
		recipients []uuid.UUID
		ids        []string
		author     = "Someone"
	)
	for _, member := range members {
		if member.UserID == m.AuthorID {
			if member.DisplayName != "" {
				author = member.DisplayName
			}
			continue
		}
		recipients = append(recipients, member.UserID)
		ids = append(ids, member.UserID.String())
	}
	if len(recipients) == 0 {
		return nil
	}

	visible, err := n.presence.Visible(ctx, ids)
	if err != nil {
		return fmt.Errorf("notify: presence: %w", err)
	}
	away := recipients[:0]
	for _, id := range recipients {
		if !visible[id.String()] {
			away = append(away, id)
		}
	}
	if len(away) == 0 {
		return nil
	}

	subs, err := n.store.PushSubscriptions(ctx, away)
	if err != nil {
		return fmt.Errorf("notify: subscriptions: %w", err)
	}
	if len(subs) == 0 {
		return nil
	}

	room, err := n.store.RoomByID(ctx, m.RoomID)
	if err != nil {
		return fmt.Errorf("notify: room: %w", err)
	}
	payload, err := json.Marshal(payloadOf(room, author, m))
	if err != nil {
		return fmt.Errorf("notify: payload: %w", err)
	}

	for _, sub := range subs {
		sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
		status, err := n.sender.Send(sendCtx, sub, payload, topicOf(m.RoomID))
		cancel()
		switch {
		case status == http.StatusNotFound || status == http.StatusGone:
			if err := n.store.DropPushEndpoint(ctx, sub.Endpoint); err != nil {
				n.log.WarnContext(ctx, "could not drop a dead push endpoint", "error", err)
			}
		case err != nil:
			n.log.WarnContext(ctx, "push delivery failed", "user_id", sub.UserID, "error", err)
		case status >= http.StatusBadRequest:
			n.log.WarnContext(ctx, "push service refused a notification", "user_id", sub.UserID, "status", status)
		}
	}
	return nil
}

func payloadOf(room domain.Room, author string, m domain.Message) Payload {
	text := previewOf(m)
	p := Payload{Title: author, Body: text, URL: "/chat#" + room.ID.String(), Tag: room.ID.String()}
	if room.Kind == domain.RoomKindGroup || room.Kind == domain.RoomKindOpen {
		p.Title = room.Name
		p.Body = author + ": " + text
	}
	return p
}

func previewOf(m domain.Message) string {
	caption := strings.TrimSpace(m.Body)
	label := ""
	switch m.Kind {
	case domain.MessageKindImage:
		label = "Photo"
	case domain.MessageKindVideo:
		label = "Video"
	case domain.MessageKindAlbum:
		label = "Album"
	case domain.MessageKindVoice:
		label = "Voice message"
	case domain.MessageKindAttachment:
		label = "File"
	}
	switch {
	case m.ForwardedFromID != nil && caption == "":
		caption = "Forwarded message"
	case caption == "":
		caption = label
	case label != "":
		caption = label + ": " + caption
	}
	if utf8.RuneCountInString(caption) > previewRunes {
		runes := []rune(caption)
		caption = string(runes[:previewRunes-1]) + "…"
	}
	return caption
}

func topicOf(roomID uuid.UUID) string {
	return strings.ReplaceAll(roomID.String(), "-", "")
}
