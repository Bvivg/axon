package proxy

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
)

// Chat forwards ChatService calls to the chat service.
type Chat struct {
	client chatv1connect.ChatServiceClient
}

var _ chatv1connect.ChatServiceHandler = (*Chat)(nil)

// NewChat returns a forwarding handler over the given client.
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

// NewChatClient builds the Connect client the gateway forwards chat through.
func NewChatClient(
	httpClient *http.Client,
	baseURL string,
	opts ...connect.ClientOption,
) chatv1connect.ChatServiceClient {
	return chatv1connect.NewChatServiceClient(httpClient, baseURL, opts...)
}
