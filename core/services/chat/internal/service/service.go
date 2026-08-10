// Package service holds the chat service's behaviour: what may be done, by
// whom, and in what order.
//
// It knows nothing about Connect, WebSockets or SQL. Sending a message means
// the same thing whether it arrived on a socket or, one day, over anything
// else, and the rule about who may send it lives here rather than in whichever
// transport happened to carry it.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

// Store is the persistence this service needs.
//
// It is an interface so the behaviour above can be tested without a database:
// the rules about membership are not rules about Postgres, and a test that
// needs a container to check one is a test that will not be run often.
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

// Service is the chat service's behaviour.
type Service struct {
	store Store
	log   *slog.Logger

	// newID generates message and room ids. Overridable so tests can predict
	// them; production leaves it nil and gets uuid.New.
	newID func() uuid.UUID
}

// Config configures a Service.
type Config struct {
	Store  Store
	Logger *slog.Logger

	// NewID overrides id generation. Tests set it; production leaves it nil.
	NewID func() uuid.UUID
}

// New returns a Service.
func New(cfg Config) (*Service, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("service: store is required")
	case cfg.Logger == nil:
		return nil, errors.New("service: logger is required")
	}

	newID := cfg.NewID
	if newID == nil {
		newID = uuid.New
	}

	return &Service{store: cfg.Store, log: cfg.Logger, newID: newID}, nil
}

// CreateRoomInput opens a room.
//
// DisplayName is the caller's name as auth knows it, resolved by the transport
// layer from the caller's own token. It is a snapshot; see domain.Member.
type CreateRoomInput struct {
	Name        string
	UserID      uuid.UUID
	DisplayName string
}

// CreateRoom opens a room with the caller in it.
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

// ListRooms returns the rooms the caller belongs to.
func (s *Service) ListRooms(ctx context.Context, userID uuid.UUID) ([]domain.Room, error) {
	rooms, err := s.store.RoomsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: list rooms: %w", err)
	}
	return rooms, nil
}

// RoomView is a room together with who is in it.
type RoomView struct {
	Room    domain.Room
	Members []domain.Member
}

// GetRoom returns a room and its members to somebody who belongs to it.
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

// JoinRoomInput adds the caller to a room.
type JoinRoomInput struct {
	RoomID      uuid.UUID
	UserID      uuid.UUID
	DisplayName string
}

// JoinRoom puts the caller in a room, reporting whether this call is what put
// them there.
//
// Any authenticated person may join any room they know the id of: rooms are
// open, and private ones are a feature this service does not have yet rather
// than one it half has. Joining again is not an error — it refreshes the name
// snapshot and reports that nothing else changed.
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

// LeaveRoom takes the caller out of a room. What they said stays.
func (s *Service) LeaveRoom(ctx context.Context, roomID, userID uuid.UUID) error {
	if err := s.store.RemoveMember(ctx, roomID, userID); err != nil {
		return fmt.Errorf("service: leave room: %w", err)
	}

	s.log.InfoContext(ctx, "left room", "room_id", roomID, "user_id", userID)

	return nil
}

// History is a page of a room's messages.
type History struct {
	Messages []domain.Message

	// HasMore reports whether more remain in the direction that was asked for.
	HasMore bool
}

// ListMessages reads a page of history for somebody who belongs to the room.
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

// SendInput is one message somebody is trying to say.
type SendInput struct {
	RoomID   uuid.UUID
	AuthorID uuid.UUID
	Body     string

	// ClientID is the id the sender minted for this message. Optional; without
	// it a resend after a dropped connection writes a second copy.
	ClientID string
}

// Send writes a message to a room, reporting whether it was already there.
//
// Membership is checked here on every message rather than only when a socket
// opens. A connection outlives the membership that justified it: somebody
// removed from a room mid-conversation must stop being able to write to it, and
// a check that ran at subscribe time cannot notice.
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

	// A resend is not news. Logging it as one would make a flaky connection look
	// like a chatty user.
	if !duplicate {
		s.log.InfoContext(ctx, "message sent",
			"room_id", in.RoomID, "user_id", in.AuthorID, "seq", message.Seq)
	}

	return message, duplicate, nil
}

// requireMember refuses everything the caller does not belong to.
//
// A room that does not exist and a room the caller is not in produce the same
// error on purpose, and the transport layer must render them the same way:
// telling them apart turns any room id into a probe for whether that room
// exists.
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
