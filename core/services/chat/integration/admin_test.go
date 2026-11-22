//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/authz"
	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func adminAction(name, targetType, targetID string) authz.Action {
	return authz.Action{Service: "chat", Name: name, Outcome: authz.Succeeded, ActorID: uuid.New(), TargetType: targetType, TargetID: targetID, Reason: "integration test"}
}

func adminEvents(t *testing.T, action string) []*eventsv1.AdminAction {
	t.Helper()
	var out []*eventsv1.AdminAction
	for _, row := range outboxRows(t, authz.Topic) {
		event := &eventsv1.AdminAction{}
		if err := proto.Unmarshal(row.payload, event); err != nil {
			t.Fatalf("decode admin action: %v", err)
		}
		if event.GetAction() == action {
			out = append(out, event)
		}
	}
	return out
}

func TestAdminRoomsFilterAndPageNewestFirst(t *testing.T) {
	r := newRepo(t)
	marker := "admin-list-" + uuid.NewString()[:8]
	var made []uuid.UUID
	for range 3 {
		room, _ := newRoom(t, r, marker)
		made = append(made, room.ID)
	}
	group, owner, members := newGroup(t, r, 1)
	say(t, r, group.ID, owner, "hello", "")

	var seen []uuid.UUID
	cursor := ""
	for page := 0; page < 3; page++ {
		rooms, next, total, err := r.AdminRooms(t.Context(), domain.AdminRoomFilter{Query: marker, Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatalf("AdminRooms: %v", err)
		}
		if total != 3 {
			t.Errorf("total = %d, want 3", total)
		}
		for _, room := range rooms {
			seen = append(seen, room.Room.ID)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != 3 || seen[0] != made[2] || seen[2] != made[0] {
		t.Errorf("paged rooms = %v, want %v newest first", seen, made)
	}

	byMember, _, total, err := r.AdminRooms(t.Context(), domain.AdminRoomFilter{Member: &members[0], Kind: domain.RoomKindGroup, Limit: 10})
	if err != nil || total != 1 || len(byMember) != 1 || byMember[0].Room.ID != group.ID {
		t.Fatalf("rooms of a member = %+v (%d), %v; want the one group", byMember, total, err)
	}
	if byMember[0].LastSeq != 1 || byMember[0].LastActivity == nil || byMember[0].Room.MemberCount != 2 {
		t.Errorf("group summary = %+v, want one message, an activity time and two members", byMember[0])
	}

	if _, _, _, err := r.AdminRooms(t.Context(), domain.AdminRoomFilter{Cursor: "not-a-cursor", Limit: 10}); err == nil {
		t.Error("a broken cursor was accepted")
	}
}

func TestAdminUploadsShowWhereEachOneWasSentNewestFirst(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "uploads")
	elsewhere, _ := newRoom(t, r, "elsewhere")
	uploader := uuid.New()

	file := uuid.New()
	if _, err := pool.Exec(t.Context(), `INSERT INTO files (id, kind, sha256, payload, object_keys, size_bytes) VALUES ($1, 'attachment', $2, '{"filename":"stored.pdf","mime":"application/pdf"}', '{}', 2048)`,
		file, file[:]); err != nil {
		t.Fatalf("insert file: %v", err)
	}
	upload := func(kind domain.MessageKind, payload string, fileID *uuid.UUID) domain.Upload {
		t.Helper()
		stored, err := r.CreateUpload(t.Context(), domain.Upload{ID: uuid.New(), UploaderID: uploader, Kind: kind, Payload: json.RawMessage(payload), FileID: fileID})
		if err != nil {
			t.Fatalf("CreateUpload(%s): %v", kind, err)
		}
		return stored
	}
	document := upload(domain.MessageKindAttachment, `{"url":"http://minio/r.pdf","filename":"report.pdf","mime":"application/pdf","size_bytes":10}`, &file)
	photo := upload(domain.MessageKindImage, `{"url":"http://minio/p.jpg","mime":"image/jpeg","size_bytes":300}`, nil)
	voice := upload(domain.MessageKindVoice, `{"url":"http://minio/v.m4a","duration_ms":900}`, nil)

	sent, _, err := r.AppendMessage(t.Context(), domain.Message{ID: uuid.New(), RoomID: room.ID, AuthorID: owner, Kind: domain.MessageKindAttachment,
		Payload: json.RawMessage(`{"url":"http://minio/r.pdf"}`), UploadID: &document.ID})
	if err != nil {
		t.Fatalf("AppendMessage(attachment): %v", err)
	}
	album, _, err := r.AppendMessage(t.Context(), domain.Message{ID: uuid.New(), RoomID: room.ID, AuthorID: owner, Kind: domain.MessageKindAlbum,
		Payload: json.RawMessage(`{"items":[]}`), AlbumUploads: []uuid.UUID{photo.ID}})
	if err != nil {
		t.Fatalf("AppendMessage(album): %v", err)
	}

	var seen []domain.AdminUpload
	cursor := ""
	for page := 0; page < 3; page++ {
		uploads, next, err := r.AdminUploads(t.Context(), domain.AdminUploadFilter{Uploader: &uploader, Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatalf("AdminUploads: %v", err)
		}
		seen = append(seen, uploads...)
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != 3 || seen[0].ID != voice.ID || seen[1].ID != photo.ID || seen[2].ID != document.ID {
		t.Fatalf("paged uploads = %+v, want voice, photo, document", seen)
	}
	if got := seen[0]; got.RoomID != nil || got.MessageID != nil || got.Kind != domain.MessageKindVoice || got.SizeBytes != 0 {
		t.Errorf("unsent voice = %+v, want no room or message", got)
	}
	if got := seen[1]; got.RoomID == nil || *got.RoomID != room.ID || got.MessageID == nil || *got.MessageID != album.ID ||
		got.ContentType != "image/jpeg" || got.SizeBytes != 300 || got.Name != "" {
		t.Errorf("album photo = %+v, want the album in %s", got, room.ID)
	}
	if got := seen[2]; got.RoomID == nil || *got.RoomID != room.ID || got.MessageID == nil || *got.MessageID != sent.ID ||
		got.Name != "report.pdf" || got.ContentType != "application/pdf" || got.SizeBytes != 2048 || got.UploaderID != uploader {
		t.Errorf("document = %+v, want the attachment message with the stored file size", got)
	}

	for name, tt := range map[string]struct {
		filter domain.AdminUploadFilter
		want   []uuid.UUID
	}{
		"in the room":       {domain.AdminUploadFilter{Uploader: &uploader, Room: &room.ID, Limit: 10}, []uuid.UUID{photo.ID, document.ID}},
		"in another room":   {domain.AdminUploadFilter{Uploader: &uploader, Room: &elsewhere.ID, Limit: 10}, nil},
		"photos only":       {domain.AdminUploadFilter{Uploader: &uploader, Kind: domain.MessageKindImage, Limit: 10}, []uuid.UUID{photo.ID}},
		"files in the room": {domain.AdminUploadFilter{Room: &room.ID, Kind: domain.MessageKindAttachment, Limit: 10}, []uuid.UUID{document.ID}},
	} {
		t.Run(name, func(t *testing.T) {
			uploads, next, err := r.AdminUploads(t.Context(), tt.filter)
			if err != nil || next != "" || len(uploads) != len(tt.want) {
				t.Fatalf("AdminUploads = %+v, %q, %v; want %v", uploads, next, err, tt.want)
			}
			for i, id := range tt.want {
				if uploads[i].ID != id {
					t.Errorf("upload %d = %s, want %s", i, uploads[i].ID, id)
				}
			}
		})
	}

	if _, _, err := r.AdminUploads(t.Context(), domain.AdminUploadFilter{Cursor: "not-a-cursor", Limit: 10}); err == nil {
		t.Error("a broken cursor was accepted")
	}
}

func TestModeratorDeletesAMessageAndRemovesAMemberWithAnAuditRow(t *testing.T) {
	r := newRepo(t)
	group, owner, members := newGroup(t, r, 2)
	said := say(t, r, group.ID, members[0], "spam", "")

	messages, more, err := r.AdminMessages(t.Context(), group.ID, 0, 10)
	if err != nil || more || len(messages) != 1 || messages[0].ID != said.ID {
		t.Fatalf("AdminMessages = %+v, %v, %v; want the one message", messages, more, err)
	}

	deleted, err := r.ModerateDeleteMessage(t.Context(), said.ID, adminAction("message_deleted", "room", group.ID.String()))
	if err != nil || !deleted.Deleted() {
		t.Fatalf("ModerateDeleteMessage = %+v, %v", deleted, err)
	}
	if got := adminEvents(t, "message_deleted"); len(got) == 0 || got[len(got)-1].GetTargetId() != group.ID.String() {
		t.Errorf("message_deleted audit rows = %v, want one for the room", got)
	}

	promoted, err := r.ModerateRemoveMember(t.Context(), group.ID, owner, adminAction("member_removed", "room", group.ID.String()))
	if err != nil {
		t.Fatalf("ModerateRemoveMember: %v", err)
	}
	if promoted == nil {
		t.Error("removing the owner promoted nobody")
	}
	if member, err := r.IsMember(t.Context(), group.ID, owner); err != nil || member {
		t.Errorf("owner still a member = %v, %v", member, err)
	}
	if got := adminEvents(t, "member_removed"); len(got) == 0 {
		t.Error("member removal left no audit row")
	}
}

func TestAModerationRollsBackWhenItsAuditRowCannotBeWritten(t *testing.T) {
	r := newRepo(t)
	group, _, members := newGroup(t, r, 1)
	said := say(t, r, group.ID, members[0], "keep me", "")
	rejectInOutbox(t, authz.Topic)

	if _, err := r.ModerateDeleteMessage(t.Context(), said.ID, adminAction("message_deleted", "room", group.ID.String())); err == nil {
		t.Fatal("the deletion succeeded without its audit row")
	}
	kept, err := r.MessageByID(t.Context(), said.ID)
	if err != nil || kept.Deleted() {
		t.Errorf("message after a failed moderation = %+v, %v; want it intact", kept, err)
	}
}

type families struct{}

func (families) ActiveFamily(context.Context, string, uuid.UUID) error { return nil }

type publisher struct{}

func (publisher) PublishCall(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (publisher) PublishCallSignal(context.Context, uuid.UUID, uuid.UUID, []byte) error { return nil }

func TestAModeratorEndsALiveCallOnceAndAuditsIt(t *testing.T) {
	r := newRepo(t)
	svc, err := calls.NewService(calls.Config{Store: r, Families: families{}, Publisher: publisher{}, Logger: logger.Discard()})
	if err != nil {
		t.Fatal(err)
	}
	a, b := callUser(t, r), callUser(t, r)
	call := mustCall(t, r, a, calls.Command{Action: "start", RoomID: directCallRoom(t, a, b), Mode: "video"})

	page, err := svc.AdminCalls(t.Context(), calls.AdminFilter{ActiveOnly: true, User: &b.actor.UserID})
	if err != nil || len(page.Calls) != 1 || page.Calls[0].ID != call.ID || page.ActiveTotal < 1 {
		t.Fatalf("active calls of the callee = %+v, %v", page, err)
	}

	ended, err := svc.Terminate(t.Context(), call.ID, adminAction("call_terminated", "call", call.ID.String()))
	if err != nil || ended.State != calls.Ended || ended.EndReason != calls.Moderated {
		t.Fatalf("Terminate = %+v, %v; want ended as moderated", ended, err)
	}
	audit := adminEvents(t, "call_terminated")
	if len(audit) == 0 || audit[len(audit)-1].GetDetails()["mode"] != "video" {
		t.Errorf("call_terminated audit rows = %v, want one carrying the call mode", audit)
	}

	if _, err := svc.Terminate(t.Context(), call.ID, adminAction("call_terminated", "call", call.ID.String())); !errors.Is(err, calls.ErrConflict) {
		t.Errorf("ending an ended call = %v, want a conflict", err)
	}
	if got := adminEvents(t, "call_terminated"); len(got) != len(audit) {
		t.Errorf("a refused termination wrote an audit row")
	}

	active, err := svc.AdminCalls(t.Context(), calls.AdminFilter{ActiveOnly: true, User: &b.actor.UserID})
	if err != nil || len(active.Calls) != 0 {
		t.Errorf("active calls after termination = %+v, %v", active.Calls, err)
	}
	if _, err := svc.AdminCalls(t.Context(), calls.AdminFilter{Cursor: "%%%"}); !errors.Is(err, calls.ErrInvalid) {
		t.Errorf("broken cursor = %v, want ErrInvalid", err)
	}
}
