package server

import (
	"encoding/json"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func toProtoRoom(r domain.Room, callerID uuid.UUID) *chatv1.Room {
	room := &chatv1.Room{
		Id:          r.ID.String(),
		Name:        r.Name,
		CreatedBy:   r.CreatedBy.String(),
		CreatedAt:   timestamppb.New(r.CreatedAt),
		MemberCount: int32(r.MemberCount),
		Kind:        toProtoRoomKind(r.Kind),
		UnreadCount: r.UnreadCount,
	}

	if peer := r.DirectPeerFor(callerID); peer != nil {
		peerID := peer.String()
		room.PeerUserId = &peerID
	}

	if r.OthersReadSeq > 0 {
		seq := r.OthersReadSeq
		room.OthersReadSeq = &seq
	}

	return room
}

func toProtoRoomKind(k domain.RoomKind) chatv1.RoomKind {
	switch k {
	case domain.RoomKindOpen:
		return chatv1.RoomKind_ROOM_KIND_OPEN
	case domain.RoomKindDirect:
		return chatv1.RoomKind_ROOM_KIND_DIRECT
	default:
		return chatv1.RoomKind_ROOM_KIND_UNSPECIFIED
	}
}

func toProtoMessageKind(k domain.MessageKind) chatv1.MessageKind {
	switch k {
	case domain.MessageKindText:
		return chatv1.MessageKind_MESSAGE_KIND_TEXT
	case domain.MessageKindVoice:
		return chatv1.MessageKind_MESSAGE_KIND_VOICE
	case domain.MessageKindAttachment:
		return chatv1.MessageKind_MESSAGE_KIND_ATTACHMENT
	case domain.MessageKindSystem:
		return chatv1.MessageKind_MESSAGE_KIND_SYSTEM
	default:
		return chatv1.MessageKind_MESSAGE_KIND_UNSPECIFIED
	}
}

func toProtoMember(m domain.Member) *chatv1.Member {
	member := &chatv1.Member{
		UserId:      m.UserID.String(),
		JoinedAt:    timestamppb.New(m.JoinedAt),
		LastReadSeq: m.LastReadSeq,
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
		Kind:     toProtoMessageKind(m.Kind),
	}

	if m.ClientID != "" {
		message.ClientId = &m.ClientID
	}
	if m.ReplyToID != nil {
		replyToID := m.ReplyToID.String()
		message.ReplyToId = &replyToID
	}
	if m.ForwardedFromID != nil {
		forwardedFromID := m.ForwardedFromID.String()
		message.ForwardedFromId = &forwardedFromID
	}

	setProtoMessagePayload(message, m.Kind, m.Payload)

	return message
}

func setProtoMessagePayload(message *chatv1.Message, kind domain.MessageKind, raw json.RawMessage) {
	switch kind {
	case domain.MessageKindVoice:
		var p domain.VoicePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return
		}
		message.Payload = &chatv1.Message_Voice{Voice: &chatv1.VoicePayload{
			DurationMs: p.DurationMS,
			Url:        p.URL,
		}}
	case domain.MessageKindAttachment:
		var p domain.AttachmentPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return
		}
		message.Payload = &chatv1.Message_Attachment{Attachment: &chatv1.AttachmentPayload{
			Url:       p.URL,
			Filename:  p.Filename,
			Mime:      p.Mime,
			SizeBytes: p.SizeBytes,
		}}
	case domain.MessageKindSystem:
		var p domain.SystemPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return
		}
		message.Payload = &chatv1.Message_System{System: &chatv1.SystemPayload{
			Event:    p.Event,
			ActorId:  p.ActorID,
			TargetId: p.TargetID,
		}}
	}
}
