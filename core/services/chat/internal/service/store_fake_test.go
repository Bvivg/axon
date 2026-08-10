package service_test

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

// fakeStore is an in-memory stand-in for the repository.
//
// It exists so the rules above it can be tested without a database. It is not a
// second implementation of the storage semantics: message ordering, the resend
// constraint and the room lock are properties of Postgres, and they are checked
// against Postgres in services/chat/integration. What is checked here is
// everything the service decides before storage is reached at all.
type fakeStore struct {
	mu sync.Mutex

	rooms    map[uuid.UUID]domain.Room
	members  map[uuid.UUID]map[uuid.UUID]domain.Member
	messages map[uuid.UUID][]domain.Message

	// failWith, when set, is returned by every method. It stands in for the
	// database being unreachable.
	failWith error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		rooms:    map[uuid.UUID]domain.Room{},
		members:  map[uuid.UUID]map[uuid.UUID]domain.Member{},
		messages: map[uuid.UUID][]domain.Message{},
	}
}

func (s *fakeStore) CreateRoom(_ context.Context, room domain.Room, creator domain.Member) (domain.Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return domain.Room{}, s.failWith
	}

	room.CreatedAt = time.Now()
	room.MemberCount = 1
	s.rooms[room.ID] = room

	creator.JoinedAt = time.Now()
	s.members[room.ID] = map[uuid.UUID]domain.Member{creator.UserID: creator}

	return room, nil
}

func (s *fakeStore) RoomByID(_ context.Context, id uuid.UUID) (domain.Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return domain.Room{}, s.failWith
	}

	room, ok := s.rooms[id]
	if !ok {
		return domain.Room{}, domain.ErrRoomNotFound
	}
	room.MemberCount = len(s.members[id])
	return room, nil
}

func (s *fakeStore) RoomsForUser(_ context.Context, userID uuid.UUID) ([]domain.Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return nil, s.failWith
	}

	var rooms []domain.Room
	for id, room := range s.rooms {
		if _, ok := s.members[id][userID]; ok {
			room.MemberCount = len(s.members[id])
			rooms = append(rooms, room)
		}
	}
	return rooms, nil
}

func (s *fakeStore) AddMember(_ context.Context, roomID uuid.UUID, m domain.Member) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return false, s.failWith
	}

	if s.members[roomID] == nil {
		s.members[roomID] = map[uuid.UUID]domain.Member{}
	}

	_, existed := s.members[roomID][m.UserID]
	m.JoinedAt = time.Now()
	s.members[roomID][m.UserID] = m

	return !existed, nil
}

func (s *fakeStore) RemoveMember(_ context.Context, roomID, userID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return s.failWith
	}

	delete(s.members[roomID], userID)
	return nil
}

func (s *fakeStore) Members(_ context.Context, roomID uuid.UUID) ([]domain.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return nil, s.failWith
	}

	members := make([]domain.Member, 0, len(s.members[roomID]))
	for _, m := range s.members[roomID] {
		members = append(members, m)
	}
	return members, nil
}

func (s *fakeStore) IsMember(_ context.Context, roomID, userID uuid.UUID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return false, s.failWith
	}

	_, ok := s.members[roomID][userID]
	return ok, nil
}

func (s *fakeStore) AppendMessage(_ context.Context, m domain.Message) (domain.Message, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return domain.Message{}, false, s.failWith
	}

	if _, ok := s.rooms[m.RoomID]; !ok {
		return domain.Message{}, false, domain.ErrRoomNotFound
	}

	if m.ClientID != "" {
		for _, existing := range s.messages[m.RoomID] {
			if existing.AuthorID == m.AuthorID && existing.ClientID == m.ClientID {
				return existing, true, nil
			}
		}
	}

	m.Seq = int64(len(s.messages[m.RoomID]) + 1)
	m.SentAt = time.Now()
	s.messages[m.RoomID] = append(s.messages[m.RoomID], m)

	return m, false, nil
}

func (s *fakeStore) ListMessages(
	_ context.Context,
	roomID uuid.UUID,
	page domain.Page,
) ([]domain.Message, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return nil, false, s.failWith
	}

	var selected []domain.Message
	for _, m := range s.messages[roomID] {
		switch {
		case page.BeforeSeq != 0 && m.Seq >= page.BeforeSeq:
		case page.AfterSeq != 0 && m.Seq <= page.AfterSeq:
		default:
			selected = append(selected, m)
		}
	}

	if len(selected) > page.Limit {
		if page.Backward() {
			return selected[len(selected)-page.Limit:], true, nil
		}
		return selected[:page.Limit], true, nil
	}
	return selected, false, nil
}
