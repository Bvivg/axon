package service_test

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type fakeStore struct {
	mu sync.Mutex

	rooms    map[uuid.UUID]domain.Room
	members  map[uuid.UUID]map[uuid.UUID]domain.Member
	messages map[uuid.UUID][]domain.Message
	uploads  map[uuid.UUID]domain.Upload

	failWith error

	failAppend error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		rooms:    map[uuid.UUID]domain.Room{},
		members:  map[uuid.UUID]map[uuid.UUID]domain.Member{},
		messages: map[uuid.UUID][]domain.Message{},
		uploads:  map[uuid.UUID]domain.Upload{},
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
		member, ok := s.members[id][userID]
		if !ok || member.HiddenAt != nil {
			continue
		}
		room.MemberCount = len(s.members[id])

		floor := member.LastReadSeq
		if member.ClearedThroughSeq > floor {
			floor = member.ClearedThroughSeq
		}
		for _, m := range s.messages[id] {
			if m.Seq > floor {
				room.UnreadCount++
			}
		}

		for otherID, other := range s.members[id] {
			if otherID != userID && other.LastReadSeq > room.OthersReadSeq {
				room.OthersReadSeq = other.LastReadSeq
			}
		}

		rooms = append(rooms, room)
	}
	return rooms, nil
}

func (s *fakeStore) DirectRoomBetween(_ context.Context, userA, userB uuid.UUID) (domain.Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return domain.Room{}, s.failWith
	}

	min, max := domain.DirectPair(userA, userB)
	for id, room := range s.rooms {
		if room.Kind != domain.RoomKindDirect || room.DirectUserMin == nil || room.DirectUserMax == nil {
			continue
		}
		if *room.DirectUserMin == min && *room.DirectUserMax == max {
			room.MemberCount = len(s.members[id])
			return room, nil
		}
	}
	return domain.Room{}, domain.ErrRoomNotFound
}

func (s *fakeStore) GetOrCreateDirectRoom(
	_ context.Context,
	newRoom domain.Room,
	memberA, memberB domain.Member,
) (domain.Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return domain.Room{}, s.failWith
	}

	min, max := domain.DirectPair(memberA.UserID, memberB.UserID)
	for id, room := range s.rooms {
		if room.Kind != domain.RoomKindDirect || room.DirectUserMin == nil || room.DirectUserMax == nil {
			continue
		}
		if *room.DirectUserMin == min && *room.DirectUserMax == max {
			room.MemberCount = len(s.members[id])
			return room, nil
		}
	}

	newRoom.CreatedAt = time.Now()
	newRoom.DirectUserMin = &min
	newRoom.DirectUserMax = &max
	newRoom.MemberCount = 2
	s.rooms[newRoom.ID] = newRoom

	memberA.JoinedAt = time.Now()
	memberB.JoinedAt = time.Now()
	s.members[newRoom.ID] = map[uuid.UUID]domain.Member{
		memberA.UserID: memberA,
		memberB.UserID: memberB,
	}

	return newRoom, nil
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

func (s *fakeStore) AddGroupMembers(
	_ context.Context,
	roomID uuid.UUID,
	members []domain.Member,
	limit int,
) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return nil, s.failWith
	}

	if s.members[roomID] == nil {
		s.members[roomID] = map[uuid.UUID]domain.Member{}
	}

	var fresh []domain.Member
	for _, m := range members {
		if _, ok := s.members[roomID][m.UserID]; !ok {
			fresh = append(fresh, m)
		}
	}
	if len(s.members[roomID])+len(fresh) > limit {
		return nil, domain.ErrGroupFull
	}

	added := make([]uuid.UUID, 0, len(fresh))
	for _, m := range fresh {
		m.JoinedAt = time.Now()
		s.members[roomID][m.UserID] = m
		added = append(added, m.UserID)
	}
	return added, nil
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

func (s *fakeStore) HideRoom(_ context.Context, roomID, userID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return s.failWith
	}

	member, ok := s.members[roomID][userID]
	if !ok {
		return domain.ErrNotAMember
	}

	var maxSeq int64
	for _, m := range s.messages[roomID] {
		if m.Seq > maxSeq {
			maxSeq = m.Seq
		}
	}

	now := time.Now()
	member.HiddenAt = &now
	member.ClearedThroughSeq = maxSeq
	s.members[roomID][userID] = member

	return nil
}

func (s *fakeStore) MarkRead(_ context.Context, roomID, userID uuid.UUID, seq int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return s.failWith
	}

	member, ok := s.members[roomID][userID]
	if !ok {
		return domain.ErrNotAMember
	}

	if seq > member.LastReadSeq {
		member.LastReadSeq = seq
		s.members[roomID][userID] = member
	}

	return nil
}

func (s *fakeStore) AppendMessage(_ context.Context, m domain.Message) (domain.Message, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return domain.Message{}, false, s.failWith
	}
	if s.failAppend != nil {
		return domain.Message{}, false, s.failAppend
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

	for id, member := range s.members[m.RoomID] {
		if member.HiddenAt != nil {
			member.HiddenAt = nil
			s.members[m.RoomID][id] = member
			m.Revealed = append(m.Revealed, id)
		}
	}

	if author, ok := s.members[m.RoomID][m.AuthorID]; ok {
		if m.Seq > author.LastReadSeq {
			author.LastReadSeq = m.Seq
		}
		s.members[m.RoomID][m.AuthorID] = author
	}

	return m, false, nil
}

func (s *fakeStore) MessageByID(_ context.Context, id uuid.UUID) (domain.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return domain.Message{}, s.failWith
	}

	for _, messages := range s.messages {
		for _, m := range messages {
			if m.ID == id {
				return m, nil
			}
		}
	}
	return domain.Message{}, domain.ErrMessageNotFound
}

func (s *fakeStore) ListMessages(
	_ context.Context,
	roomID, callerID uuid.UUID,
	page domain.Page,
) ([]domain.Message, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return nil, false, s.failWith
	}

	clearedThrough := s.members[roomID][callerID].ClearedThroughSeq

	var selected []domain.Message
	for _, m := range s.messages[roomID] {
		if m.Seq <= clearedThrough {
			continue
		}
		if len(page.Kinds) > 0 && !slices.Contains(page.Kinds, m.Kind) {
			continue
		}
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

func (s *fakeStore) CreateUpload(_ context.Context, u domain.Upload) (domain.Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return domain.Upload{}, s.failWith
	}

	u.CreatedAt = time.Now()
	s.uploads[u.ID] = u
	return u, nil
}

func (s *fakeStore) UploadByID(_ context.Context, id uuid.UUID) (domain.Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return domain.Upload{}, s.failWith
	}

	u, ok := s.uploads[id]
	if !ok {
		return domain.Upload{}, domain.ErrUploadNotFound
	}
	return u, nil
}

func (s *fakeStore) CreateGroup(
	_ context.Context,
	room domain.Room,
	owner domain.Member,
	members []domain.Member,
) (domain.Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return domain.Room{}, s.failWith
	}

	room.CreatedAt = time.Now()
	room.MemberCount = 1 + len(members)
	s.rooms[room.ID] = room

	owner.Role = domain.MemberRoleOwner
	owner.JoinedAt = time.Now()
	s.members[room.ID] = map[uuid.UUID]domain.Member{owner.UserID: owner}
	for _, m := range members {
		m.Role = domain.MemberRoleMember
		m.JoinedAt = time.Now()
		s.members[room.ID][m.UserID] = m
	}

	return room, nil
}

func (s *fakeStore) RenameRoom(_ context.Context, roomID uuid.UUID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	room, ok := s.rooms[roomID]
	if !ok {
		return domain.ErrRoomNotFound
	}
	room.Name = name
	s.rooms[roomID] = room
	return nil
}

func (s *fakeStore) MemberRole(_ context.Context, roomID, userID uuid.UUID) (domain.MemberRole, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return "", s.failWith
	}

	member, ok := s.members[roomID][userID]
	if !ok {
		return "", domain.ErrNotAMember
	}
	if member.Role == "" {
		return domain.MemberRoleMember, nil
	}
	return member.Role, nil
}

func (s *fakeStore) LeaveGroup(_ context.Context, roomID, userID uuid.UUID) (*uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return nil, s.failWith
	}

	delete(s.members[roomID], userID)

	var earliest *domain.Member
	for _, m := range s.members[roomID] {
		if m.Role == domain.MemberRoleOwner {
			return nil, nil
		}
		if earliest == nil || m.JoinedAt.Before(earliest.JoinedAt) ||
			(m.JoinedAt.Equal(earliest.JoinedAt) && m.UserID.String() < earliest.UserID.String()) {
			candidate := m
			earliest = &candidate
		}
	}
	if earliest == nil {
		return nil, nil
	}

	earliest.Role = domain.MemberRoleOwner
	s.members[roomID][earliest.UserID] = *earliest
	promoted := earliest.UserID
	return &promoted, nil
}

func (s *fakeStore) HistoryStart(_ context.Context, roomID, userID uuid.UUID) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return 0, s.failWith
	}

	member, ok := s.members[roomID][userID]
	if !ok {
		return 0, domain.ErrNotAMember
	}
	return member.ClearedThroughSeq, nil
}

func (s *fakeStore) EditMessage(_ context.Context, id uuid.UUID, body string) (domain.Message, error) {
	return s.changeMessage(id, func(m *domain.Message) {
		now := time.Now()
		m.Body = body
		m.EditedAt = &now
	})
}

func (s *fakeStore) DeleteMessage(_ context.Context, id uuid.UUID) (domain.Message, error) {
	return s.changeMessage(id, func(m *domain.Message) {
		now := time.Now()
		m.Body = ""
		m.Payload = nil
		m.DeletedAt = &now
	})
}

func (s *fakeStore) changeMessage(id uuid.UUID, change func(*domain.Message)) (domain.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failWith != nil {
		return domain.Message{}, s.failWith
	}

	for roomID, messages := range s.messages {
		for i, m := range messages {
			if m.ID != id {
				continue
			}
			if m.DeletedAt != nil {
				return domain.Message{}, domain.ErrMessageDeleted
			}
			change(&m)
			s.messages[roomID][i] = m
			return m, nil
		}
	}
	return domain.Message{}, domain.ErrMessageNotFound
}
