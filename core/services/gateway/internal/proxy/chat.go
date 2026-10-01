package proxy

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
)

type Chat struct {
	client chatv1connect.ChatServiceClient
}

var _ chatv1connect.ChatServiceHandler = (*Chat)(nil)

func NewChat(client chatv1connect.ChatServiceClient) (*Chat, error) {
	if client == nil {
		return nil, errors.New("proxy: chat client is required")
	}
	return &Chat{client: client}, nil
}

func (c *Chat) CreateRoom(
	ctx context.Context,
	req *connect.Request[chatv1.CreateRoomRequest],
) (*connect.Response[chatv1.CreateRoomResponse], error) {
	return c.client.CreateRoom(ctx, forward(ctx, req))
}

func (c *Chat) ListRooms(
	ctx context.Context,
	req *connect.Request[chatv1.ListRoomsRequest],
) (*connect.Response[chatv1.ListRoomsResponse], error) {
	return c.client.ListRooms(ctx, forward(ctx, req))
}

func (c *Chat) GetRoom(
	ctx context.Context,
	req *connect.Request[chatv1.GetRoomRequest],
) (*connect.Response[chatv1.GetRoomResponse], error) {
	return c.client.GetRoom(ctx, forward(ctx, req))
}

func (c *Chat) JoinRoom(
	ctx context.Context,
	req *connect.Request[chatv1.JoinRoomRequest],
) (*connect.Response[chatv1.JoinRoomResponse], error) {
	return c.client.JoinRoom(ctx, forward(ctx, req))
}

func (c *Chat) LeaveRoom(
	ctx context.Context,
	req *connect.Request[chatv1.LeaveRoomRequest],
) (*connect.Response[chatv1.LeaveRoomResponse], error) {
	return c.client.LeaveRoom(ctx, forward(ctx, req))
}

func (c *Chat) ListMessages(
	ctx context.Context,
	req *connect.Request[chatv1.ListMessagesRequest],
) (*connect.Response[chatv1.ListMessagesResponse], error) {
	return c.client.ListMessages(ctx, forward(ctx, req))
}

func (c *Chat) HideRoom(
	ctx context.Context,
	req *connect.Request[chatv1.HideRoomRequest],
) (*connect.Response[chatv1.HideRoomResponse], error) {
	return c.client.HideRoom(ctx, forward(ctx, req))
}

func (c *Chat) GetDirectRoom(
	ctx context.Context,
	req *connect.Request[chatv1.GetDirectRoomRequest],
) (*connect.Response[chatv1.GetDirectRoomResponse], error) {
	return c.client.GetDirectRoom(ctx, forward(ctx, req))
}

func (c *Chat) MarkRead(
	ctx context.Context,
	req *connect.Request[chatv1.MarkReadRequest],
) (*connect.Response[chatv1.MarkReadResponse], error) {
	return c.client.MarkRead(ctx, forward(ctx, req))
}

func (c *Chat) CreateGroup(
	ctx context.Context,
	req *connect.Request[chatv1.CreateGroupRequest],
) (*connect.Response[chatv1.CreateGroupResponse], error) {
	return c.client.CreateGroup(ctx, forward(ctx, req))
}

func (c *Chat) AddGroupMembers(
	ctx context.Context,
	req *connect.Request[chatv1.AddGroupMembersRequest],
) (*connect.Response[chatv1.AddGroupMembersResponse], error) {
	return c.client.AddGroupMembers(ctx, forward(ctx, req))
}

func (c *Chat) RemoveGroupMember(
	ctx context.Context,
	req *connect.Request[chatv1.RemoveGroupMemberRequest],
) (*connect.Response[chatv1.RemoveGroupMemberResponse], error) {
	return c.client.RemoveGroupMember(ctx, forward(ctx, req))
}

func (c *Chat) RenameGroup(
	ctx context.Context,
	req *connect.Request[chatv1.RenameGroupRequest],
) (*connect.Response[chatv1.RenameGroupResponse], error) {
	return c.client.RenameGroup(ctx, forward(ctx, req))
}

func NewChatClient(
	httpClient *http.Client,
	baseURL string,
	opts ...connect.ClientOption,
) chatv1connect.ChatServiceClient {
	return chatv1connect.NewChatServiceClient(httpClient, baseURL, opts...)
}

func (c *Chat) ListMessageReaders(
	ctx context.Context,
	req *connect.Request[chatv1.ListMessageReadersRequest],
) (*connect.Response[chatv1.ListMessageReadersResponse], error) {
	return c.client.ListMessageReaders(ctx, forward(ctx, req))
}

func (c *Chat) ForwardMessages(
	ctx context.Context,
	req *connect.Request[chatv1.ForwardMessagesRequest],
) (*connect.Response[chatv1.ForwardMessagesResponse], error) {
	return c.client.ForwardMessages(ctx, forward(ctx, req))
}

func (c *Chat) GetPushKey(
	ctx context.Context,
	req *connect.Request[chatv1.GetPushKeyRequest],
) (*connect.Response[chatv1.GetPushKeyResponse], error) {
	return c.client.GetPushKey(ctx, forward(ctx, req))
}

func (c *Chat) SubscribePush(
	ctx context.Context,
	req *connect.Request[chatv1.SubscribePushRequest],
) (*connect.Response[chatv1.SubscribePushResponse], error) {
	return c.client.SubscribePush(ctx, forward(ctx, req))
}

func (c *Chat) UnsubscribePush(
	ctx context.Context,
	req *connect.Request[chatv1.UnsubscribePushRequest],
) (*connect.Response[chatv1.UnsubscribePushResponse], error) {
	return c.client.UnsubscribePush(ctx, forward(ctx, req))
}

func (c *Chat) SearchMessages(
	ctx context.Context,
	req *connect.Request[chatv1.SearchMessagesRequest],
) (*connect.Response[chatv1.SearchMessagesResponse], error) {
	return c.client.SearchMessages(ctx, forward(ctx, req))
}
