package service

import (
	"context"
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

	AddMember(ctx context.Context, roomID uuid.UUID, m domain.Member) (bool, error)
	RemoveMember(ctx context.Context, roomID, userID uuid.UUID) error
	Members(ctx context.Context, roomID uuid.UUID) ([]domain.Member, error)
	IsMember(ctx context.Context, roomID, userID uuid.UUID) (bool, error)

	AppendMessage(ctx context.Context, m domain.Message) (domain.Message, bool, error)
	ListMessages(ctx context.Context, roomID uuid.UUID, page domain.Page) ([]domain.Message, bool, error)
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
		domain.Room{ID: s.newID(), Name: name, CreatedBy: in.UserID},
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

	messages, more, err := s.store.ListMessages(ctx, roomID, page)
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
}

func (s *Service) Send(ctx context.Context, in SendInput) (domain.Message, bool, error) {
	body, err := domain.ValidateMessageBody(in.Body)
	if err != nil {
		return domain.Message{}, false, err
	}

	clientID, err := domain.ValidateClientID(in.ClientID)
	if err != nil {
		return domain.Message{}, false, err
	}

	if err := s.requireMember(ctx, in.RoomID, in.AuthorID); err != nil {
		return domain.Message{}, false, err
	}

	message, duplicate, err := s.store.AppendMessage(ctx, domain.Message{
		ID:       s.newID(),
		RoomID:   in.RoomID,
		AuthorID: in.AuthorID,
		Body:     body,
		ClientID: clientID,
	})
	if err != nil {
		return domain.Message{}, false, fmt.Errorf("service: send message: %w", err)
	}

	if !duplicate {
		s.log.InfoContext(ctx, "message sent",
			"room_id", in.RoomID, "user_id", in.AuthorID, "seq", message.Seq)

		s.events.MessageSent(context.WithoutCancel(ctx), message)
	}

	return message, duplicate, nil
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
