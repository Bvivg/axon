package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type Store interface {
	CreateRoom(ctx context.Context, room domain.Room, creator domain.Member) (domain.Room, error)
	RoomByID(ctx context.Context, id uuid.UUID) (domain.Room, error)
	RoomsForUser(ctx context.Context, userID uuid.UUID) ([]domain.Room, error)
	DirectRoomBetween(ctx context.Context, userA, userB uuid.UUID) (domain.Room, error)
	GetOrCreateDirectRoom(ctx context.Context, newRoom domain.Room, memberA, memberB domain.Member) (domain.Room, error)

	AddMember(ctx context.Context, roomID uuid.UUID, m domain.Member) (bool, error)
	RemoveMember(ctx context.Context, roomID, userID uuid.UUID) error
	Members(ctx context.Context, roomID uuid.UUID) ([]domain.Member, error)
	IsMember(ctx context.Context, roomID, userID uuid.UUID) (bool, error)
	HideRoom(ctx context.Context, roomID, userID uuid.UUID) error
	MarkRead(ctx context.Context, roomID, userID uuid.UUID, seq int64) error

	AppendMessage(ctx context.Context, m domain.Message) (domain.Message, bool, error)
	MessageByID(ctx context.Context, id uuid.UUID) (domain.Message, error)
	ListMessages(ctx context.Context, roomID, callerID uuid.UUID, page domain.Page) ([]domain.Message, bool, error)
}

type Events interface {
	MessageSent(ctx context.Context, m domain.Message)
}

type Service struct {
	store  Store
	events Events
	log    *slog.Logger

	newID func() uuid.UUID
}

type Config struct {
	Store  Store
	Logger *slog.Logger

	Events Events

	NewID func() uuid.UUID
}

func New(cfg Config) (*Service, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("service: store is required")
	case cfg.Events == nil:
		return nil, errors.New("service: an event publisher is required (use events.Discard for none)")
	case cfg.Logger == nil:
		return nil, errors.New("service: logger is required")
	}

	newID := cfg.NewID
	if newID == nil {
		newID = uuid.New
	}

	return &Service{store: cfg.Store, events: cfg.Events, log: cfg.Logger, newID: newID}, nil
}

type CreateRoomInput struct {
	Name        string
	UserID      uuid.UUID
	DisplayName string
}

func (s *Service) CreateRoom(ctx context.Context, in CreateRoomInput) (domain.Room, error) {
	name, err := domain.ValidateRoomName(in.Name)
	if err != nil {
		return domain.Room{}, err
	}

	room, err := s.store.CreateRoom(ctx,
		domain.Room{ID: s.newID(), Name: name, CreatedBy: in.UserID, Kind: domain.RoomKindOpen},
		domain.Member{UserID: in.UserID, DisplayName: domain.ValidateDisplayName(in.DisplayName)},
	)
	if err != nil {
		return domain.Room{}, fmt.Errorf("service: create room: %w", err)
	}

	s.log.InfoContext(ctx, "room created", "room_id", room.ID, "user_id", in.UserID)

	return room, nil
}

func (s *Service) ListRooms(ctx context.Context, userID uuid.UUID) ([]domain.Room, error) {
	rooms, err := s.store.RoomsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: list rooms: %w", err)
	}
	return rooms, nil
}

type RoomView struct {
	Room    domain.Room
	Members []domain.Member
}

func (s *Service) GetRoom(ctx context.Context, roomID, userID uuid.UUID) (RoomView, error) {
	if err := s.requireMember(ctx, roomID, userID); err != nil {
		return RoomView{}, err
	}

	room, err := s.store.RoomByID(ctx, roomID)
	if err != nil {
		return RoomView{}, fmt.Errorf("service: get room: %w", err)
	}

	members, err := s.store.Members(ctx, roomID)
	if err != nil {
		return RoomView{}, fmt.Errorf("service: get room members: %w", err)
	}

	return RoomView{Room: room, Members: members}, nil
}

func (s *Service) GetDirectRoom(ctx context.Context, callerID, peerID uuid.UUID) (*domain.Room, error) {
	room, err := s.store.DirectRoomBetween(ctx, callerID, peerID)
	switch {
	case err == nil:
		return &room, nil
	case errors.Is(err, domain.ErrRoomNotFound):
		return nil, nil
	default:
		return nil, fmt.Errorf("service: get direct room: %w", err)
	}
}

type JoinRoomInput struct {
	RoomID      uuid.UUID
	UserID      uuid.UUID
	DisplayName string
}

func (s *Service) JoinRoom(ctx context.Context, in JoinRoomInput) (domain.Room, bool, error) {
	room, err := s.store.RoomByID(ctx, in.RoomID)
	if err != nil {
		return domain.Room{}, false, fmt.Errorf("service: join room: %w", err)
	}

	if room.Kind == domain.RoomKindDirect {
		return domain.Room{}, false, domain.ErrDirectRoomNotJoinable
	}

	joined, err := s.store.AddMember(ctx, in.RoomID, domain.Member{
		UserID:      in.UserID,
		DisplayName: domain.ValidateDisplayName(in.DisplayName),
	})
	if err != nil {
		return domain.Room{}, false, fmt.Errorf("service: join room: %w", err)
	}

	if joined {
		room.MemberCount++
		s.log.InfoContext(ctx, "joined room", "room_id", in.RoomID, "user_id", in.UserID)
	}

	return room, joined, nil
}

func (s *Service) LeaveRoom(ctx context.Context, roomID, userID uuid.UUID) error {
	if err := s.store.RemoveMember(ctx, roomID, userID); err != nil {
		return fmt.Errorf("service: leave room: %w", err)
	}

	s.log.InfoContext(ctx, "left room", "room_id", roomID, "user_id", userID)

	return nil
}

func (s *Service) HideRoom(ctx context.Context, roomID, userID uuid.UUID) error {
	if err := s.requireMember(ctx, roomID, userID); err != nil {
		return err
	}

	if err := s.store.HideRoom(ctx, roomID, userID); err != nil {
		return fmt.Errorf("service: hide room: %w", err)
	}

	s.log.InfoContext(ctx, "room hidden", "room_id", roomID, "user_id", userID)

	return nil
}

func (s *Service) MarkRead(ctx context.Context, roomID, userID uuid.UUID, seq int64) error {
	if err := s.requireMember(ctx, roomID, userID); err != nil {
		return err
	}

	if err := s.store.MarkRead(ctx, roomID, userID, seq); err != nil {
		return fmt.Errorf("service: mark read: %w", err)
	}

	return nil
}

type History struct {
	Messages []domain.Message

	HasMore bool
}

func (s *Service) ListMessages(
	ctx context.Context,
	roomID, userID uuid.UUID,
	page domain.Page,
) (History, error) {
	page, err := domain.ValidatePage(page)
	if err != nil {
		return History{}, err
	}

	if err := s.requireMember(ctx, roomID, userID); err != nil {
		return History{}, err
	}

	messages, more, err := s.store.ListMessages(ctx, roomID, userID, page)
	if err != nil {
		return History{}, fmt.Errorf("service: list messages: %w", err)
	}

	return History{Messages: messages, HasMore: more}, nil
}

type SendInput struct {
	RoomID   uuid.UUID
	AuthorID uuid.UUID
	Body     string

	ClientID string

	Kind    string
	Payload json.RawMessage

	ReplyToID       string
	ForwardedFromID string
}

func (s *Service) Send(ctx context.Context, in SendInput) (domain.Message, bool, error) {
	if err := s.requireMember(ctx, in.RoomID, in.AuthorID); err != nil {
		return domain.Message{}, false, err
	}

	return s.appendValidated(ctx, in.RoomID, in.AuthorID, sendCore{
		Body:            in.Body,
		ClientID:        in.ClientID,
		Kind:            in.Kind,
		Payload:         in.Payload,
		ReplyToID:       in.ReplyToID,
		ForwardedFromID: in.ForwardedFromID,
	})
}

type SendDirectInput struct {
	FromUserID      uuid.UUID
	FromDisplayName string
	ToUserID        uuid.UUID

	Body     string
	ClientID string

	Kind    string
	Payload json.RawMessage

	ReplyToID       string
	ForwardedFromID string
}

func (s *Service) SendDirect(ctx context.Context, in SendDirectInput) (domain.Message, domain.Room, bool, error) {
	if in.FromUserID == in.ToUserID {
		return domain.Message{}, domain.Room{}, false, domain.ErrCannotMessageSelf
	}

	room, err := s.store.GetOrCreateDirectRoom(ctx,
		domain.Room{ID: s.newID(), Name: "", CreatedBy: in.FromUserID, Kind: domain.RoomKindDirect},
		domain.Member{UserID: in.FromUserID, DisplayName: domain.ValidateDisplayName(in.FromDisplayName)},
		domain.Member{UserID: in.ToUserID, DisplayName: ""},
	)
	if err != nil {
		return domain.Message{}, domain.Room{}, false, fmt.Errorf("service: open direct room: %w", err)
	}

	message, duplicate, err := s.appendValidated(ctx, room.ID, in.FromUserID, sendCore{
		Body:            in.Body,
		ClientID:        in.ClientID,
		Kind:            in.Kind,
		Payload:         in.Payload,
		ReplyToID:       in.ReplyToID,
		ForwardedFromID: in.ForwardedFromID,
	})
	if err != nil {
		return domain.Message{}, domain.Room{}, false, err
	}

	return message, room, duplicate, nil
}

func (s *Service) AppendSystemEvent(
	ctx context.Context,
	roomID uuid.UUID,
	event string,
	actorID, targetID *uuid.UUID,
) (domain.Message, error) {
	payload := domain.SystemPayload{Event: event}
	if actorID != nil {
		actor := actorID.String()
		payload.ActorID = &actor
	}
	if targetID != nil {
		target := targetID.String()
		payload.TargetID = &target
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Message{}, fmt.Errorf("service: marshal system payload: %w", err)
	}
	validated, err := domain.ValidatePayload(domain.MessageKindSystem, raw)
	if err != nil {
		return domain.Message{}, err
	}

	author := uuid.Nil
	if actorID != nil {
		author = *actorID
	}

	message, duplicate, err := s.store.AppendMessage(ctx, domain.Message{
		ID:       s.newID(),
		RoomID:   roomID,
		AuthorID: author,
		Body:     "",
		Kind:     domain.MessageKindSystem,
		Payload:  validated,
	})
	if err != nil {
		return domain.Message{}, fmt.Errorf("service: append system event: %w", err)
	}

	if !duplicate {
		s.log.InfoContext(ctx, "system event", "room_id", roomID, "event", event)
		s.events.MessageSent(context.WithoutCancel(ctx), message)
	}

	return message, nil
}

type sendCore struct {
	Body     string
	ClientID string

	Kind    string
	Payload json.RawMessage

	ReplyToID       string
	ForwardedFromID string
}

func (s *Service) appendValidated(
	ctx context.Context,
	roomID, authorID uuid.UUID,
	in sendCore,
) (domain.Message, bool, error) {
	kind, err := domain.ValidateMessageKind(in.Kind)
	if err != nil {
		return domain.Message{}, false, err
	}
	if kind == domain.MessageKindSystem {
		return domain.Message{}, false, domain.ErrSystemKindNotSendable
	}

	var body string
	if kind == domain.MessageKindText {
		body, err = domain.ValidateMessageBody(in.Body)
	} else {
		body, err = domain.ValidateCaption(in.Body)
	}
	if err != nil {
		return domain.Message{}, false, err
	}

	payload, err := domain.ValidatePayload(kind, in.Payload)
	if err != nil {
		return domain.Message{}, false, err
	}

	clientID, err := domain.ValidateClientID(in.ClientID)
	if err != nil {
		return domain.Message{}, false, err
	}

	replyToID, err := s.resolveReplyTarget(ctx, roomID, in.ReplyToID)
	if err != nil {
		return domain.Message{}, false, err
	}

	forwardedFromID, err := s.resolveForwardSource(ctx, authorID, in.ForwardedFromID)
	if err != nil {
		return domain.Message{}, false, err
	}

	message, duplicate, err := s.store.AppendMessage(ctx, domain.Message{
		ID:              s.newID(),
		RoomID:          roomID,
		AuthorID:        authorID,
		Body:            body,
		ClientID:        clientID,
		Kind:            kind,
		Payload:         payload,
		ReplyToID:       replyToID,
		ForwardedFromID: forwardedFromID,
	})
	if err != nil {
		return domain.Message{}, false, fmt.Errorf("service: send message: %w", err)
	}

	if !duplicate {
		s.log.InfoContext(ctx, "message sent", "room_id", roomID, "user_id", authorID, "seq", message.Seq)

		s.events.MessageSent(context.WithoutCancel(ctx), message)
	}

	return message, duplicate, nil
}

func (s *Service) resolveReplyTarget(ctx context.Context, roomID uuid.UUID, raw string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}

	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, domain.ErrInvalidReplyTarget
	}

	target, err := s.store.MessageByID(ctx, id)
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrMessageNotFound):
		return nil, domain.ErrInvalidReplyTarget
	default:
		return nil, fmt.Errorf("service: resolve reply target: %w", err)
	}

	if target.RoomID != roomID {
		return nil, domain.ErrInvalidReplyTarget
	}

	return &id, nil
}

func (s *Service) resolveForwardSource(ctx context.Context, authorID uuid.UUID, raw string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}

	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, domain.ErrInvalidForwardTarget
	}

	source, err := s.store.MessageByID(ctx, id)
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrMessageNotFound):
		return nil, domain.ErrInvalidForwardTarget
	default:
		return nil, fmt.Errorf("service: resolve forward source: %w", err)
	}

	member, err := s.store.IsMember(ctx, source.RoomID, authorID)
	if err != nil {
		return nil, fmt.Errorf("service: check forward access: %w", err)
	}
	if !member {
		return nil, domain.ErrInvalidForwardTarget
	}

	return &id, nil
}

func (s *Service) requireMember(ctx context.Context, roomID, userID uuid.UUID) error {
	member, err := s.store.IsMember(ctx, roomID, userID)
	if err != nil {
		return fmt.Errorf("service: check membership: %w", err)
	}
	if !member {
		return domain.ErrNotAMember
	}
	return nil
}
