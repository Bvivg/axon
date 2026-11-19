package proxy

import (
	"context"

	"connectrpc.com/connect"

	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
)

type Game struct {
	client gamev1connect.GameServiceClient
}

func NewGame(client connect.HTTPClient, url string, opts ...connect.ClientOption) *Game {
	return &Game{client: gamev1connect.NewGameServiceClient(client, url, opts...)}
}

func (p *Game) CreateSession(ctx context.Context, req *connect.Request[gamev1.CreateSessionRequest]) (*connect.Response[gamev1.CreateSessionResponse], error) {
	return p.client.CreateSession(ctx, forward(ctx, req))
}

func (p *Game) JoinSession(ctx context.Context, req *connect.Request[gamev1.JoinSessionRequest]) (*connect.Response[gamev1.JoinSessionResponse], error) {
	return p.client.JoinSession(ctx, forward(ctx, req))
}

func (p *Game) GetSession(ctx context.Context, req *connect.Request[gamev1.GetSessionRequest]) (*connect.Response[gamev1.GetSessionResponse], error) {
	return p.client.GetSession(ctx, forward(ctx, req))
}

func (p *Game) ListSessions(ctx context.Context, req *connect.Request[gamev1.ListSessionsRequest]) (*connect.Response[gamev1.ListSessionsResponse], error) {
	return p.client.ListSessions(ctx, forward(ctx, req))
}

func (p *Game) FindMatch(ctx context.Context, req *connect.Request[gamev1.FindMatchRequest]) (*connect.Response[gamev1.FindMatchResponse], error) {
	return p.client.FindMatch(ctx, forward(ctx, req))
}

func (p *Game) CancelMatch(ctx context.Context, req *connect.Request[gamev1.CancelMatchRequest]) (*connect.Response[gamev1.CancelMatchResponse], error) {
	return p.client.CancelMatch(ctx, forward(ctx, req))
}

func (p *Game) GetMatch(ctx context.Context, req *connect.Request[gamev1.GetMatchRequest]) (*connect.Response[gamev1.GetMatchResponse], error) {
	return p.client.GetMatch(ctx, forward(ctx, req))
}

func (p *Game) GetRatings(ctx context.Context, req *connect.Request[gamev1.GetRatingsRequest]) (*connect.Response[gamev1.GetRatingsResponse], error) {
	return p.client.GetRatings(ctx, forward(ctx, req))
}

func (p *Game) LeaveSession(ctx context.Context, req *connect.Request[gamev1.LeaveSessionRequest]) (*connect.Response[gamev1.LeaveSessionResponse], error) {
	return p.client.LeaveSession(ctx, forward(ctx, req))
}
