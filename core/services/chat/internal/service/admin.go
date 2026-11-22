package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type Moderation interface {
	AdminRooms(ctx context.Context, f domain.AdminRoomFilter) ([]domain.AdminRoom, string, int64, error)
	AdminRoom(ctx context.Context, id uuid.UUID) (domain.AdminRoom, error)
	AdminMessages(ctx context.Context, roomID uuid.UUID, beforeSeq int64, limit int) ([]domain.Message, bool, error)
	RecordAdminAction(ctx context.Context, action authz.Action) error
	ModerateDeleteMessage(ctx context.Context, id uuid.UUID, action authz.Action) (domain.Message, error)
	ModerateRemoveMember(ctx context.Context, roomID, userID uuid.UUID, action authz.Action) (*uuid.UUID, error)
}

var ErrModerationUnavailable = errors.New("service: moderation is not configured")

func (s *Service) moderation() (Moderation, error) {
	m, ok := s.store.(Moderation)
	if !ok {
		return nil, ErrModerationUnavailable
	}
	return m, nil
}

type AdminRoomView struct {
	Room    domain.AdminRoom
	Members []domain.Member
}

type AdminHistory struct {
	Messages []domain.Message
	NextSeq  *int64
}

func (s *Service) AdminRooms(ctx context.Context, f domain.AdminRoomFilter) ([]domain.AdminRoom, string, int64, error) {
	moderation, err := s.moderation()
	if err != nil {
		return nil, "", 0, err
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 25
	}
	return moderation.AdminRooms(ctx, f)
}

func (s *Service) AdminRoom(ctx context.Context, id uuid.UUID) (AdminRoomView, error) {
	moderation, err := s.moderation()
	if err != nil {
		return AdminRoomView{}, err
	}
	room, err := moderation.AdminRoom(ctx, id)
	if err != nil {
		return AdminRoomView{}, err
	}
	members, err := s.store.Members(ctx, id)
	if err != nil {
		return AdminRoomView{}, fmt.Errorf("service: admin room members: %w", err)
	}
	return AdminRoomView{Room: room, Members: members}, nil
}

func (s *Service) AdminMessages(ctx context.Context, roomID uuid.UUID, beforeSeq int64, limit int, action authz.Action) (AdminHistory, error) {
	moderation, err := s.moderation()
	if err != nil {
		return AdminHistory{}, err
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if _, err := moderation.AdminRoom(ctx, roomID); err != nil {
		return AdminHistory{}, err
	}
	action.Details = map[string]string{"before_seq": strconv.FormatInt(beforeSeq, 10)}
	if err := moderation.RecordAdminAction(ctx, action); err != nil {
		return AdminHistory{}, fmt.Errorf("service: record message view: %w", err)
	}
	messages, more, err := moderation.AdminMessages(ctx, roomID, beforeSeq, limit)
	if err != nil {
		return AdminHistory{}, err
	}
	out := AdminHistory{Messages: messages}
	if more && len(messages) > 0 {
		next := messages[0].Seq
		out.NextSeq = &next
	}
	return out, nil
}

func (s *Service) ModerateDeleteMessage(ctx context.Context, messageID uuid.UUID, action authz.Action) (domain.Message, error) {
	moderation, err := s.moderation()
	if err != nil {
		return domain.Message{}, err
	}
	target, err := s.store.MessageByID(ctx, messageID)
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrMessageNotFound):
		return domain.Message{}, err
	default:
		return domain.Message{}, fmt.Errorf("service: load message: %w", err)
	}
	if target.Kind == domain.MessageKindSystem {
		return domain.Message{}, domain.ErrMessageNotEditable
	}
	if target.Deleted() {
		return domain.Message{}, domain.ErrMessageDeleted
	}

	action.TargetType, action.TargetID = "room", target.RoomID.String()
	action.Details = map[string]string{"message_id": target.ID.String(), "author_id": target.AuthorID.String(), "kind": string(target.Kind)}
	deleted, err := moderation.ModerateDeleteMessage(ctx, messageID, action)
	if err != nil {
		return domain.Message{}, err
	}
	s.log.InfoContext(ctx, "message removed by a moderator", "room_id", deleted.RoomID, "message_id", deleted.ID, "actor_id", action.ActorID)
	return deleted, nil
}

func (s *Service) ModerateRemoveMember(ctx context.Context, roomID, targetID uuid.UUID, action authz.Action) (GroupChange, error) {
	moderation, err := s.moderation()
	if err != nil {
		return GroupChange{}, err
	}
	room, err := s.store.RoomByID(ctx, roomID)
	if err != nil {
		return GroupChange{}, err
	}
	if room.Kind != domain.RoomKindGroup {
		return GroupChange{}, domain.ErrNotAGroup
	}
	member, err := s.store.IsMember(ctx, roomID, targetID)
	if err != nil {
		return GroupChange{}, fmt.Errorf("service: check group member: %w", err)
	}
	if !member {
		return GroupChange{}, &domain.ValidationError{Field: "user_id", Reason: "is not in this group"}
	}

	action.Details = map[string]string{"user_id": targetID.String()}
	promoted, err := moderation.ModerateRemoveMember(ctx, roomID, targetID, action)
	if err != nil {
		return GroupChange{}, err
	}
	s.log.InfoContext(ctx, "member removed by a moderator", "room_id", roomID, "target_id", targetID, "actor_id", action.ActorID)

	room.MemberCount--
	change := GroupChange{Room: room, Removed: []uuid.UUID{targetID}}
	actor := action.ActorID
	s.record(ctx, &change, SystemEvent{RoomID: roomID, Event: domain.SystemMemberRemoved, ActorID: &actor, TargetID: &targetID})
	if promoted != nil {
		s.log.InfoContext(ctx, "group ownership moved after a moderator removal", "room_id", roomID, "owner_id", *promoted)
	}
	return change, nil
}

type UploadAudit interface {
	AdminUploads(ctx context.Context, f domain.AdminUploadFilter) ([]domain.AdminUpload, string, error)
}

func (s *Service) AdminUploads(ctx context.Context, f domain.AdminUploadFilter) ([]domain.AdminUpload, string, error) {
	audit, ok := s.store.(UploadAudit)
	if !ok {
		return nil, "", ErrModerationUnavailable
	}
	switch f.Kind {
	case "", domain.MessageKindVoice, domain.MessageKindAttachment, domain.MessageKindImage, domain.MessageKindVideo:
	default:
		return nil, "", &domain.ValidationError{Field: "kind", Reason: "must be voice, attachment, image or video"}
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 25
	}
	return audit.AdminUploads(ctx, f)
}
