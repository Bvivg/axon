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
	case domain.RoomKindGroup:
		return chatv1.RoomKind_ROOM_KIND_GROUP
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
	case domain.MessageKindImage:
		return chatv1.MessageKind_MESSAGE_KIND_IMAGE
	case domain.MessageKindVideo:
		return chatv1.MessageKind_MESSAGE_KIND_VIDEO
	case domain.MessageKindSystem:
		return chatv1.MessageKind_MESSAGE_KIND_SYSTEM
	default:
		return chatv1.MessageKind_MESSAGE_KIND_UNSPECIFIED
	}
}

func fromProtoMessageKind(k chatv1.MessageKind) domain.MessageKind {
	switch k {
	case chatv1.MessageKind_MESSAGE_KIND_TEXT:
		return domain.MessageKindText
	case chatv1.MessageKind_MESSAGE_KIND_VOICE:
		return domain.MessageKindVoice
	case chatv1.MessageKind_MESSAGE_KIND_ATTACHMENT:
		return domain.MessageKindAttachment
	case chatv1.MessageKind_MESSAGE_KIND_IMAGE:
		return domain.MessageKindImage
	case chatv1.MessageKind_MESSAGE_KIND_VIDEO:
		return domain.MessageKindVideo
	case chatv1.MessageKind_MESSAGE_KIND_SYSTEM:
		return domain.MessageKindSystem
	default:
		return ""
	}
}

func fromProtoMessageKinds(kinds []chatv1.MessageKind) []domain.MessageKind {
	if len(kinds) == 0 {
		return nil
	}
	out := make([]domain.MessageKind, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, fromProtoMessageKind(k))
	}
	return out
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

	switch m.Role {
	case domain.MemberRoleOwner:
		role := chatv1.MemberRole_MEMBER_ROLE_OWNER
		member.Role = &role
	case domain.MemberRoleMember:
		role := chatv1.MemberRole_MEMBER_ROLE_MEMBER
		member.Role = &role
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
	if m.ForwardOriginAuthorID != nil {
		origin := m.ForwardOriginAuthorID.String()
		message.ForwardedFromAuthorId = &origin
	}
	if m.ReplyTo != nil {
		message.ReplyTo = &chatv1.ReplyPreview{
			Id:       m.ReplyTo.ID.String(),
			AuthorId: m.ReplyTo.AuthorID.String(),
			Kind:     toProtoMessageKind(m.ReplyTo.Kind),
			Body:     m.ReplyTo.Body,
			Deleted:  m.ReplyTo.Deleted,
		}
	}
	if m.EditedAt != nil {
		message.EditedAt = timestamppb.New(*m.EditedAt)
	}
	if m.DeletedAt != nil {
		message.DeletedAt = timestamppb.New(*m.DeletedAt)
		return message
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
		voice := &chatv1.VoicePayload{
			DurationMs: p.DurationMS,
			Url:        p.URL,
		}
		if p.Mime != "" {
			voice.Mime = &p.Mime
		}
		if p.SizeBytes > 0 {
			voice.SizeBytes = &p.SizeBytes
		}
		message.Payload = &chatv1.Message_Voice{Voice: voice}
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
	case domain.MessageKindImage:
		var p domain.ImagePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return
		}
		message.Payload = &chatv1.Message_Image{Image: &chatv1.ImagePayload{
			Url:          p.URL,
			ThumbnailUrl: p.ThumbnailURL,
			Width:        int32(p.Width),
			Height:       int32(p.Height),
			SizeBytes:    p.SizeBytes,
			Mime:         p.Mime,
		}}
	case domain.MessageKindVideo:
		var p domain.VideoPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return
		}
		message.Payload = &chatv1.Message_Video{Video: &chatv1.VideoPayload{
			Url:        p.URL,
			PosterUrl:  p.PosterURL,
			Width:      int32(p.Width),
			Height:     int32(p.Height),
			DurationMs: p.DurationMS,
			SizeBytes:  p.SizeBytes,
			Mime:       p.Mime,
		}}
	case domain.MessageKindSystem:
		var p domain.SystemPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return
		}
		message.Payload = &chatv1.Message_System{System: &chatv1.SystemPayload{
			Event:     p.Event,
			ActorId:   p.ActorID,
			TargetId:  p.TargetID,
			TargetIds: p.TargetIDs,
		}}
	}
}
