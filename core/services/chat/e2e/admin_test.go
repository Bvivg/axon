//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
)

const moderationReason = "e2e moderation check"

func grantAdmin(t *testing.T, userID string) {
	t.Helper()
	dsn := os.Getenv("ADMIN_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADMIN_POSTGRES_DSN is not set; run via make test-e2e")
	}
	pool, err := postgres.Connect(context.Background(), postgres.Config{DSN: config.NewSecret(dsn)}, logger.Discard())
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(t.Context(), `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name = 'admin' ON CONFLICT DO NOTHING`, userID); err != nil {
		t.Fatalf("grant admin: %v", err)
	}
}

func (u *user) console() chatv1connect.ChatAdminServiceClient {
	n := nextCaller.Add(1)
	addr := fmt.Sprintf("10.%d.%d.%d", (n>>16)&0xff, (n>>8)&0xff, n&0xff)
	return chatv1connect.NewChatAdminServiceClient(&http.Client{Transport: bearer{token: u.token, addr: addr}}, gatewayURL)
}

func TestAModeratorRemovesAMessageAndAMemberLive(t *testing.T) {
	owner, member, admin := newUser(t), newUser(t), newUser(t)
	grantAdmin(t, admin.id)
	name := "moderated-" + uuid.NewString()[:8]
	group := owner.createGroup(t, name, member)

	_, err := member.console().ListChatRooms(context.Background(), connect.NewRequest(&chatv1.ListChatRoomsRequest{}))
	wantCode(t, "a plain member listing every chat", err, connect.CodePermissionDenied)

	ownerSocket, ms := owner.connect(t), member.connect(t)
	ownerSocket.subscribe(group.GetId(), 0)
	ms.subscribe(group.GetId(), 0)
	ms.send(inbound{Type: typeSend, RoomID: group.GetId(), ClientID: "mod-1", Body: "buy cheap followers"})
	ms.expect(typeAck)
	sent := ownerSocket.expect(typeMessage)

	console := admin.console()
	rooms, err := console.ListChatRooms(context.Background(), connect.NewRequest(&chatv1.ListChatRoomsRequest{Query: name}))
	if err != nil {
		t.Fatalf("ListChatRooms: %v", err)
	}
	if rooms.Msg.GetTotal() != 1 || rooms.Msg.GetRooms()[0].GetRoom().GetId() != group.GetId() || rooms.Msg.GetRooms()[0].GetRoom().GetMemberCount() != 2 {
		t.Fatalf("rooms named %s = %v", name, rooms.Msg.GetRooms())
	}

	_, err = console.ListChatRoomMessages(context.Background(), connect.NewRequest(&chatv1.ListChatRoomMessagesRequest{RoomId: group.GetId()}))
	wantCode(t, "reading history without a reason", err, connect.CodeInvalidArgument)

	history, err := console.ListChatRoomMessages(context.Background(), connect.NewRequest(&chatv1.ListChatRoomMessagesRequest{RoomId: group.GetId(), Reason: moderationReason}))
	if err != nil {
		t.Fatalf("ListChatRoomMessages: %v", err)
	}
	found := false
	for _, m := range history.Msg.GetMessages() {
		found = found || m.GetId() == sent.Message.ID
	}
	if !found {
		t.Fatalf("history %v lacks the reported message", history.Msg.GetMessages())
	}

	if _, err := console.DeleteChatMessage(context.Background(), connect.NewRequest(&chatv1.DeleteChatMessageRequest{MessageId: sent.Message.ID, Reason: moderationReason})); err != nil {
		t.Fatalf("DeleteChatMessage: %v", err)
	}
	if deleted := ownerSocket.expect(typeMessageUpdated); deleted.Message.ID != sent.Message.ID || deleted.Message.DeletedAt == nil {
		t.Errorf("owner saw %+v, want the message deleted", deleted.Message)
	}
	_, err = console.DeleteChatMessage(context.Background(), connect.NewRequest(&chatv1.DeleteChatMessageRequest{MessageId: sent.Message.ID, Reason: moderationReason}))
	wantCode(t, "deleting a deleted message", err, connect.CodeFailedPrecondition)

	if _, err := console.RemoveChatMember(context.Background(), connect.NewRequest(&chatv1.RemoveChatMemberRequest{RoomId: group.GetId(), UserId: member.id, Reason: moderationReason})); err != nil {
		t.Fatalf("RemoveChatMember: %v", err)
	}
	_, err = member.chat.GetRoom(context.Background(), connect.NewRequest(&chatv1.GetRoomRequest{RoomId: group.GetId()}))
	wantCode(t, "the removed member opening the group", err, connect.CodeNotFound)

	detail, err := console.GetChatRoom(context.Background(), connect.NewRequest(&chatv1.GetChatRoomRequest{RoomId: group.GetId()}))
	if err != nil {
		t.Fatalf("GetChatRoom: %v", err)
	}
	if members := detail.Msg.GetMembers(); len(members) != 1 || members[0].GetUserId() != owner.id {
		t.Errorf("members after removal = %v, want only the owner", members)
	}
}

func TestAModeratorSeesWhereAnUploadWasSent(t *testing.T) {
	owner, member, admin := newUser(t), newUser(t), newUser(t)
	grantAdmin(t, admin.id)
	group := owner.createGroup(t, "uploads-"+uuid.NewString()[:8], member)

	_, err := member.console().ListChatUploads(context.Background(), connect.NewRequest(&chatv1.ListChatUploadsRequest{}))
	wantCode(t, "a plain member listing every upload", err, connect.CodePermissionDenied)

	up, status := member.upload(t, "file", "minutes.txt", []byte("moderated upload listing"))
	if status != http.StatusOK {
		t.Fatalf("upload status = %d, want 200", status)
	}
	ms := member.connect(t)
	ms.subscribe(group.GetId(), 0)
	sent := ms.sendUpload(group.GetId(), up.UploadID, "")

	_, err = admin.console().ListChatUploads(context.Background(), connect.NewRequest(&chatv1.ListChatUploadsRequest{UploaderId: "someone"}))
	wantCode(t, "listing uploads of a malformed uploader", err, connect.CodeInvalidArgument)

	listed, err := admin.console().ListChatUploads(context.Background(), connect.NewRequest(&chatv1.ListChatUploadsRequest{UploaderId: member.id, RoomId: group.GetId()}))
	if err != nil {
		t.Fatalf("ListChatUploads: %v", err)
	}
	uploads := listed.Msg.GetUploads()
	if len(uploads) != 1 || listed.Msg.GetNextCursor() != "" {
		t.Fatalf("uploads of %s = %v, want the one file", member.id, uploads)
	}
	got := uploads[0]
	if got.GetId() != up.UploadID || got.GetKind() != "attachment" || got.GetName() != "minutes.txt" || got.GetSizeBytes() != int64(len("moderated upload listing")) ||
		got.GetRoomId() != group.GetId() || got.GetMessageId() != sent.Message.ID || got.GetUploaderId() != member.id {
		t.Errorf("upload = %v, want the file sent to the group as message %s", got, sent.Message.ID)
	}
}
