package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type EditInput struct {
	MessageID uuid.UUID
	UserID    uuid.UUID
	Body      string
}

func (s *Service) EditMessage(ctx context.Context, in EditInput) (domain.Message, error) {
	target, err := s.ownMessage(ctx, in.MessageID, in.UserID)
	if err != nil {
		return domain.Message{}, err
	}
	if !target.Editable() {
		return domain.Message{}, domain.ErrMessageNotEditable
	}

	var body string
	if target.Kind == domain.MessageKindText {
		body, err = domain.ValidateMessageBody(in.Body)
	} else {
		body, err = domain.ValidateCaption(in.Body)
	}
	if err != nil {
		return domain.Message{}, err
	}

	if body == target.Body {
		return target, nil
	}

	edited, err := s.store.EditMessage(ctx, target.ID, body)
	if err != nil {
		if errors.Is(err, domain.ErrMessageDeleted) {
			return domain.Message{}, err
		}
		return domain.Message{}, fmt.Errorf("service: edit message: %w", err)
	}

	s.log.InfoContext(ctx, "message edited", "room_id", edited.RoomID, "message_id", edited.ID, "user_id", in.UserID)

	return edited, nil
}

func (s *Service) DeleteMessage(ctx context.Context, messageID, userID uuid.UUID) (domain.Message, error) {
	target, err := s.ownMessage(ctx, messageID, userID)
	if err != nil {
		return domain.Message{}, err
	}
	if target.Kind == domain.MessageKindSystem {
		return domain.Message{}, domain.ErrMessageNotEditable
	}

	deleted, err := s.store.DeleteMessage(ctx, target.ID)
	if err != nil {
		if errors.Is(err, domain.ErrMessageDeleted) {
			return domain.Message{}, err
		}
		return domain.Message{}, fmt.Errorf("service: delete message: %w", err)
	}

	s.log.InfoContext(ctx, "message deleted", "room_id", deleted.RoomID, "message_id", deleted.ID, "user_id", userID)

	return deleted, nil
}

func (s *Service) ownMessage(ctx context.Context, messageID, userID uuid.UUID) (domain.Message, error) {
	target, err := s.store.MessageByID(ctx, messageID)
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrMessageNotFound):
		return domain.Message{}, err
	default:
		return domain.Message{}, fmt.Errorf("service: load message: %w", err)
	}

	if err := s.requireMember(ctx, target.RoomID, userID); err != nil {
		if errors.Is(err, domain.ErrNotAMember) {
			return domain.Message{}, domain.ErrMessageNotFound
		}
		return domain.Message{}, err
	}

	if target.AuthorID != userID {
		return domain.Message{}, domain.ErrNotMessageAuthor
	}
	if target.Deleted() {
		return domain.Message{}, domain.ErrMessageDeleted
	}

	return target, nil
}
