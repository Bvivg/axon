package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
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
	Parts      []ForwardPart
}

type ForwardPart struct {
	MessageID string
	Positions []int
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
	positions := make(map[uuid.UUID][]int, len(in.Parts))
	for _, part := range in.Parts {
		id, err := uuid.Parse(part.MessageID)
		if err != nil || !slices.Contains(sources, id) {
			return nil, &domain.ValidationError{Field: "parts", Reason: "names a message that is not forwarded"}
		}
		if len(part.Positions) == 0 || len(part.Positions) > domain.MaxAlbumItems {
			return nil, &domain.ValidationError{Field: "parts", Reason: "picks 1 to 10 album items"}
		}
		positions[id] = part.Positions
	}
	for _, sourceID := range sources {
		source, err := s.resolveForwardSource(ctx, in.AuthorID, sourceID.String())
		if err != nil {
			return nil, err
		}
		if _, picked := positions[sourceID]; picked && source.Kind != domain.MessageKindAlbum {
			return nil, &domain.ValidationError{Field: "parts", Reason: "only album items can be picked"}
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
				RoomID:           roomID,
				AuthorID:         in.AuthorID,
				ClientID:         forwardClientID(batch, roomID, sourceID.String()),
				ForwardedFromID:  sourceID.String(),
				ForwardPositions: positions[sourceID],
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
