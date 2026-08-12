package server

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func toProtoRoom(r domain.Room) *chatv1.Room {
	return &chatv1.Room{
		Id:          r.ID.String(),
		Name:        r.Name,
		CreatedBy:   r.CreatedBy.String(),
		CreatedAt:   timestamppb.New(r.CreatedAt),
		MemberCount: int32(r.MemberCount),
	}
}

func toProtoMember(m domain.Member) *chatv1.Member {
	member := &chatv1.Member{
		UserId:   m.UserID.String(),
		JoinedAt: timestamppb.New(m.JoinedAt),
	}

	if m.DisplayName != "" {
		member.DisplayName = &m.DisplayName
	}

	return member
}

func toProtoMessage(m domain.Message) *chatv1.Message {
	message := &chatv1.Message{
		Id:       m.ID.String(),
		RoomId:   m.RoomID.String(),
		AuthorId: m.AuthorID.String(),
		Body:     m.Body,
		Seq:      m.Seq,
		SentAt:   timestamppb.New(m.SentAt),
	}

	if m.ClientID != "" {
		message.ClientId = &m.ClientID
	}

	return message
}
