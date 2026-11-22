package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/events"
	"github.com/bvivg/axon/core/services/chat/internal/service"
)

type uploadLedger struct {
	service.Store
	uploads []domain.AdminUpload
	err     error
	asked   *domain.AdminUploadFilter
}

func (l *uploadLedger) AdminUploads(_ context.Context, f domain.AdminUploadFilter) ([]domain.AdminUpload, string, error) {
	l.asked = &f
	if l.err != nil {
		return nil, "", l.err
	}
	return l.uploads, "cursor-2", nil
}

func TestListChatUploads(t *testing.T) {
	uploader, room, message := uuid.New(), uuid.New(), uuid.New()
	created := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	linked := domain.AdminUpload{
		ID: uuid.New(), UploaderID: uploader, Kind: domain.MessageKindAttachment, Name: "report.pdf", ContentType: "application/pdf",
		SizeBytes: 2048, CreatedAt: created, RoomID: &room, MessageID: &message,
	}
	orphan := domain.AdminUpload{ID: uuid.New(), UploaderID: uploader, Kind: domain.MessageKindImage, CreatedAt: created}

	tests := map[string]struct {
		request *chatv1.ListChatUploadsRequest
		store   *uploadLedger
		code    connect.Code
		asked   *domain.AdminUploadFilter
		want    []*chatv1.AdminUpload
	}{
		"filters reach the store and rows map back": {
			request: &chatv1.ListChatUploadsRequest{UploaderId: uploader.String(), RoomId: room.String(), Kind: "attachment", Cursor: "cursor-1", PageSize: 10},
			store:   &uploadLedger{uploads: []domain.AdminUpload{linked}},
			asked:   &domain.AdminUploadFilter{Uploader: &uploader, Room: &room, Kind: domain.MessageKindAttachment, Cursor: "cursor-1", Limit: 10},
			want: []*chatv1.AdminUpload{{
				Id: linked.ID.String(), UploaderId: uploader.String(), Kind: "attachment", Name: "report.pdf", ContentType: "application/pdf",
				SizeBytes: 2048, RoomId: room.String(), MessageId: message.String(),
			}},
		},
		"an unsent upload has no room or message": {
			request: &chatv1.ListChatUploadsRequest{},
			store:   &uploadLedger{uploads: []domain.AdminUpload{orphan}},
			asked:   &domain.AdminUploadFilter{Limit: 25},
			want:    []*chatv1.AdminUpload{{Id: orphan.ID.String(), UploaderId: uploader.String(), Kind: "image"}},
		},
		"a malformed uploader": {
			request: &chatv1.ListChatUploadsRequest{UploaderId: "ada"},
			store:   &uploadLedger{},
			code:    connect.CodeInvalidArgument,
		},
		"a malformed room": {
			request: &chatv1.ListChatUploadsRequest{RoomId: "lobby"},
			store:   &uploadLedger{},
			code:    connect.CodeInvalidArgument,
		},
		"an unknown kind": {
			request: &chatv1.ListChatUploadsRequest{Kind: "album"},
			store:   &uploadLedger{},
			code:    connect.CodeInvalidArgument,
		},
		"a broken cursor": {
			request: &chatv1.ListChatUploadsRequest{Cursor: "%%%"},
			store:   &uploadLedger{err: &domain.ValidationError{Field: "cursor", Reason: "is not valid"}},
			code:    connect.CodeInvalidArgument,
			asked:   &domain.AdminUploadFilter{Cursor: "%%%", Limit: 25},
		},
		"a store failure": {
			request: &chatv1.ListChatUploadsRequest{},
			store:   &uploadLedger{err: errors.New("connection refused")},
			code:    connect.CodeInternal,
			asked:   &domain.AdminUploadFilter{Limit: 25},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			svc, err := service.New(service.Config{Store: tt.store, Events: events.Discard(), Logger: logger.Discard()})
			if err != nil {
				t.Fatalf("service.New: %v", err)
			}
			admin := NewChatAdmin(&Handler{svc: svc, log: logger.Discard()})

			got, err := admin.ListChatUploads(t.Context(), connect.NewRequest(tt.request))
			switch {
			case tt.code == 0 && err != nil:
				t.Fatalf("ListChatUploads: %v", err)
			case tt.code != 0 && connect.CodeOf(err) != tt.code:
				t.Fatalf("code = %v (%v), want %v", connect.CodeOf(err), err, tt.code)
			}
			if !sameFilter(tt.store.asked, tt.asked) {
				t.Errorf("store asked %+v, want %+v", tt.store.asked, tt.asked)
			}
			if tt.code != 0 {
				return
			}
			if got.Msg.GetNextCursor() != "cursor-2" || len(got.Msg.GetUploads()) != len(tt.want) {
				t.Fatalf("response = %v", got.Msg)
			}
			for i, want := range tt.want {
				have := got.Msg.GetUploads()[i]
				if have.GetId() != want.GetId() || have.GetUploaderId() != want.GetUploaderId() || have.GetKind() != want.GetKind() ||
					have.GetName() != want.GetName() || have.GetContentType() != want.GetContentType() || have.GetSizeBytes() != want.GetSizeBytes() ||
					have.GetRoomId() != want.GetRoomId() || have.GetMessageId() != want.GetMessageId() || !have.GetCreatedAt().AsTime().Equal(created) {
					t.Errorf("upload %d = %v, want %v", i, have, want)
				}
			}
		})
	}
}

func sameFilter(have, want *domain.AdminUploadFilter) bool {
	if have == nil || want == nil {
		return have == want
	}
	same := func(a, b *uuid.UUID) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	return same(have.Uploader, want.Uploader) && same(have.Room, want.Room) &&
		have.Kind == want.Kind && have.Cursor == want.Cursor && have.Limit == want.Limit
}
