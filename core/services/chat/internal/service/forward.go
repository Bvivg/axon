package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type ForwardInput struct {
	AuthorID   uuid.UUID
	MessageIDs []string
	RoomIDs    []string
	Comment    string
	ClientID   string
}

type Forwarded struct {
	Message   domain.Message
	Duplicate bool
}

func (s *Service) ForwardMessages(ctx context.Context, in ForwardInput) ([]Forwarded, error) {
	sources, err := domain.ValidateIDList("message_ids", in.MessageIDs, domain.MaxForwardMessages)
	if err != nil {
		return nil, err
	}
	rooms, err := domain.ValidateIDList("room_ids", in.RoomIDs, domain.MaxForwardTargets)
	if err != nil {
		return nil, err
	}
	comment := strings.TrimSpace(in.Comment)
	if comment != "" {
		if comment, err = domain.ValidateMessageBody(comment); err != nil {
			return nil, err
		}
	}
	batch, err := domain.ValidateClientID(in.ClientID)
	if err != nil {
		return nil, err
	}

	for _, roomID := range rooms {
		if err := s.requireMember(ctx, roomID, in.AuthorID); err != nil {
			return nil, err
		}
	}
	for _, sourceID := range sources {
		if _, err := s.resolveForwardSource(ctx, in.AuthorID, sourceID.String()); err != nil {
			return nil, err
		}
	}

	var sent []Forwarded
	for _, roomID := range rooms {
		if comment != "" {
			message, duplicate, err := s.Send(ctx, SendInput{
				RoomID:   roomID,
				AuthorID: in.AuthorID,
				Body:     comment,
				ClientID: forwardClientID(batch, roomID, "comment"),
			})
			if err != nil {
				return sent, err
			}
			sent = append(sent, Forwarded{Message: message, Duplicate: duplicate})
		}
		for _, sourceID := range sources {
			message, duplicate, err := s.Send(ctx, SendInput{
				RoomID:          roomID,
				AuthorID:        in.AuthorID,
				ClientID:        forwardClientID(batch, roomID, sourceID.String()),
				ForwardedFromID: sourceID.String(),
			})
			if err != nil {
				return sent, err
			}
			sent = append(sent, Forwarded{Message: message, Duplicate: duplicate})
		}
	}

	s.log.InfoContext(ctx, "messages forwarded",
		"user_id", in.AuthorID, "messages", len(sources), "rooms", len(rooms), "sent", len(sent))

	return sent, nil
}

func forwardClientID(batch string, roomID uuid.UUID, part string) string {
	if batch == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(batch + "|" + roomID.String() + "|" + part))
	return "fwd-" + hex.EncodeToString(sum[:20])
}
