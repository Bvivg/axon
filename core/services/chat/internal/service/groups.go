package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type GroupMember struct {
	UserID      uuid.UUID
	DisplayName string
}

type CreateGroupInput struct {
	Name    string
	Owner   GroupMember
	Members []GroupMember
}

type GroupChange struct {
	Room domain.Room

	Events []domain.Message

	Added   []uuid.UUID
	Removed []uuid.UUID
}

func (s *Service) CreateGroup(ctx context.Context, in CreateGroupInput) (GroupChange, error) {
	name, err := domain.ValidateRoomName(in.Name)
	if err != nil {
		return GroupChange{}, err
	}
	if len(in.Members) == 0 {
		return GroupChange{}, &domain.ValidationError{Field: "member_user_ids", Reason: "needs at least one person"}
	}
	if len(in.Members)+1 > domain.MaxGroupMembers {
		return GroupChange{}, &domain.ValidationError{Field: "member_user_ids", Reason: "has too many people"}
	}

	members := make([]domain.Member, 0, len(in.Members))
	added := []uuid.UUID{in.Owner.UserID}
	for _, m := range in.Members {
		members = append(members, domain.Member{UserID: m.UserID, DisplayName: domain.ValidateDisplayName(m.DisplayName)})
		added = append(added, m.UserID)
	}

	room, err := s.store.CreateGroup(ctx,
		domain.Room{ID: s.newID(), Name: name, CreatedBy: in.Owner.UserID, Kind: domain.RoomKindGroup},
		domain.Member{UserID: in.Owner.UserID, DisplayName: domain.ValidateDisplayName(in.Owner.DisplayName)},
		members,
	)
	if err != nil {
		return GroupChange{}, fmt.Errorf("service: create group: %w", err)
	}

	s.log.InfoContext(ctx, "group created", "room_id", room.ID, "user_id", in.Owner.UserID, "members", len(members)+1)

	change := GroupChange{Room: room, Added: added}
	s.record(ctx, &change, SystemEvent{
		RoomID:  room.ID,
		Event:   domain.SystemGroupCreated,
		ActorID: &in.Owner.UserID,
		Body:    name,
	})

	return change, nil
}

func (s *Service) AddGroupMembers(
	ctx context.Context,
	roomID, actorID uuid.UUID,
	people []GroupMember,
) (GroupChange, error) {
	room, err := s.requireGroupOwner(ctx, roomID, actorID)
	if err != nil {
		return GroupChange{}, err
	}
	if len(people) == 0 {
		return GroupChange{}, &domain.ValidationError{Field: "user_ids", Reason: "needs at least one person"}
	}

	members := make([]domain.Member, 0, len(people))
	for _, person := range people {
		members = append(members, domain.Member{
			UserID:      person.UserID,
			DisplayName: domain.ValidateDisplayName(person.DisplayName),
			Role:        domain.MemberRoleMember,
		})
	}

	added, err := s.store.AddGroupMembers(ctx, roomID, members, domain.MaxGroupMembers)
	switch {
	case errors.Is(err, domain.ErrGroupFull):
		return GroupChange{}, &domain.ValidationError{Field: "user_ids", Reason: "would make the group too large"}
	case err != nil:
		return GroupChange{}, fmt.Errorf("service: add group members: %w", err)
	}

	change := GroupChange{Room: room, Added: added}
	change.Room.MemberCount += len(added)
	if len(added) > 0 {
		s.record(ctx, &change, SystemEvent{
			RoomID:    roomID,
			Event:     domain.SystemMemberAdded,
			ActorID:   &actorID,
			TargetIDs: added,
		})
	}

	s.log.InfoContext(ctx, "group members added", "room_id", roomID, "user_id", actorID, "added", len(change.Added))

	return change, nil
}

func (s *Service) RemoveGroupMember(ctx context.Context, roomID, actorID, targetID uuid.UUID) (GroupChange, error) {
	room, err := s.requireGroupOwner(ctx, roomID, actorID)
	if err != nil {
		return GroupChange{}, err
	}
	if targetID == actorID {
		return GroupChange{}, &domain.ValidationError{Field: "user_id", Reason: "is you; leave the group instead"}
	}

	member, err := s.store.IsMember(ctx, roomID, targetID)
	if err != nil {
		return GroupChange{}, fmt.Errorf("service: check group member: %w", err)
	}
	if !member {
		return GroupChange{}, &domain.ValidationError{Field: "user_id", Reason: "is not in this group"}
	}

	if err := s.store.RemoveMember(ctx, roomID, targetID); err != nil {
		return GroupChange{}, fmt.Errorf("service: remove group member: %w", err)
	}

	s.log.InfoContext(ctx, "group member removed", "room_id", roomID, "user_id", actorID, "target_id", targetID)

	room.MemberCount--
	change := GroupChange{Room: room, Removed: []uuid.UUID{targetID}}
	s.record(ctx, &change, SystemEvent{
		RoomID:   roomID,
		Event:    domain.SystemMemberRemoved,
		ActorID:  &actorID,
		TargetID: &targetID,
	})

	return change, nil
}

func (s *Service) RenameGroup(ctx context.Context, roomID, actorID uuid.UUID, rawName string) (GroupChange, error) {
	room, err := s.requireGroupOwner(ctx, roomID, actorID)
	if err != nil {
		return GroupChange{}, err
	}

	name, err := domain.ValidateRoomName(rawName)
	if err != nil {
		return GroupChange{}, err
	}
	if name == room.Name {
		return GroupChange{Room: room}, nil
	}

	if err := s.store.RenameRoom(ctx, roomID, name); err != nil {
		return GroupChange{}, fmt.Errorf("service: rename group: %w", err)
	}
	room.Name = name

	s.log.InfoContext(ctx, "group renamed", "room_id", roomID, "user_id", actorID)

	change := GroupChange{Room: room}
	s.record(ctx, &change, SystemEvent{
		RoomID:  roomID,
		Event:   domain.SystemGroupRenamed,
		ActorID: &actorID,
		Body:    name,
	})

	return change, nil
}

func (s *Service) LeaveRoom(ctx context.Context, roomID, userID uuid.UUID) (GroupChange, error) {
	if err := s.requireMember(ctx, roomID, userID); err != nil {
		if errors.Is(err, domain.ErrNotAMember) {
			return GroupChange{}, nil
		}
		return GroupChange{}, err
	}

	room, err := s.store.RoomByID(ctx, roomID)
	if err != nil {
		return GroupChange{}, fmt.Errorf("service: leave room: %w", err)
	}
	if room.Kind == domain.RoomKindDirect {
		return GroupChange{}, domain.ErrNotAGroup
	}

	change := GroupChange{Room: room, Removed: []uuid.UUID{userID}}
	if room.Kind != domain.RoomKindGroup {
		if err := s.store.RemoveMember(ctx, roomID, userID); err != nil {
			return GroupChange{}, fmt.Errorf("service: leave room: %w", err)
		}
		s.log.InfoContext(ctx, "left room", "room_id", roomID, "user_id", userID)
		return change, nil
	}

	promoted, err := s.store.LeaveGroup(ctx, roomID, userID)
	if err != nil {
		return GroupChange{}, fmt.Errorf("service: leave group: %w", err)
	}
	s.log.InfoContext(ctx, "left room", "room_id", roomID, "user_id", userID)
	if promoted != nil {
		s.log.InfoContext(ctx, "group owner changed", "room_id", roomID, "owner_id", *promoted)
	}

	s.record(ctx, &change, SystemEvent{
		RoomID:  roomID,
		Event:   domain.SystemMemberLeft,
		ActorID: &userID,
	})

	return change, nil
}

func (s *Service) record(ctx context.Context, change *GroupChange, in SystemEvent) {
	event, err := s.AppendSystemEvent(ctx, in)
	if err != nil {
		s.log.ErrorContext(ctx, "could not record a group change", "room_id", in.RoomID, "event", in.Event, "error", err)
		return
	}
	change.Events = append(change.Events, event)
}

func (s *Service) requireGroupOwner(ctx context.Context, roomID, userID uuid.UUID) (domain.Room, error) {
	role, err := s.store.MemberRole(ctx, roomID, userID)
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrNotAMember):
		return domain.Room{}, err
	default:
		return domain.Room{}, fmt.Errorf("service: check group role: %w", err)
	}

	room, err := s.store.RoomByID(ctx, roomID)
	if err != nil {
		return domain.Room{}, fmt.Errorf("service: load group: %w", err)
	}
	if room.Kind != domain.RoomKindGroup {
		return domain.Room{}, domain.ErrNotAGroup
	}
	if role != domain.MemberRoleOwner {
		return domain.Room{}, domain.ErrNotGroupOwner
	}

	return room, nil
}
