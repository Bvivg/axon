package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
)

const (
	HopHeader    = "X-Axon-Owner-Hop"
	routeTimeout = 5 * time.Second
	routePause   = 50 * time.Millisecond
)

var errMoved = connect.NewError(connect.CodeUnavailable, errors.New("the game is moving to another server, retry"))

type peers struct {
	client *http.Client
	mu     sync.Mutex
	games  map[string]gamev1connect.GameServiceClient
	owners map[string]gamev1connect.GameOwnerServiceClient
}

func newPeers(client *http.Client) *peers {
	return &peers{client: client, games: map[string]gamev1connect.GameServiceClient{}, owners: map[string]gamev1connect.GameOwnerServiceClient{}}
}

func (p *peers) game(address string) gamev1connect.GameServiceClient {
	p.mu.Lock()
	defer p.mu.Unlock()
	client, ok := p.games[address]
	if !ok {
		client = gamev1connect.NewGameServiceClient(p.client, "http://"+address)
		p.games[address] = client
	}
	return client
}

func (p *peers) owner(address string) gamev1connect.GameOwnerServiceClient {
	p.mu.Lock()
	defer p.mu.Unlock()
	client, ok := p.owners[address]
	if !ok {
		client = gamev1connect.NewGameOwnerServiceClient(p.client, "http://"+address)
		p.owners[address] = client
	}
	return client
}

func retriable(err error) bool {
	return errors.Is(err, errMoved) || connect.CodeOf(err) == connect.CodeUnavailable
}

func pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(routePause):
		return nil
	}
}

func (h *Handler) ownerAddress(ctx context.Context, id uuid.UUID) (string, bool, error) {
	for {
		if h.owner.Serves(id) {
			return "", true, nil
		}
		if address, ok := h.routes.Address(h.owner.PartitionOf(id)); ok && address != h.self {
			return address, false, nil
		}
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-h.routes.Changed():
		case <-time.After(routePause):
		}
	}
}

func onOwner[Req, Res any](
	h *Handler,
	ctx context.Context,
	id uuid.UUID,
	req *connect.Request[Req],
	local func() (*connect.Response[Res], error),
	remote func(gamev1connect.GameServiceClient, context.Context, *connect.Request[Req]) (*connect.Response[Res], error),
) (*connect.Response[Res], error) {
	hopped := req.Header().Get(HopHeader) != ""
	routing, cancel := context.WithTimeout(ctx, routeTimeout)
	defer cancel()
	for {
		if hopped && !h.owner.Serves(id) {
			return nil, errMoved
		}
		address, mine, err := h.ownerAddress(routing, id)
		if err != nil {
			return nil, errMoved
		}
		var res *connect.Response[Res]
		if mine {
			res, err = local()
		} else {
			forwarded := connect.NewRequest(req.Msg)
			forwarded.Header().Set("Authorization", req.Header().Get("Authorization"))
			forwarded.Header().Set(HopHeader, "1")
			if user := req.Header().Get(TestUserHeader); user != "" && h.hooks {
				forwarded.Header().Set(TestUserHeader, user)
			}
			res, err = remote(h.peers.game(address), ctx, forwarded)
		}
		if err == nil || !retriable(err) || hopped {
			return res, err
		}
		if pause(routing) != nil {
			return nil, errMoved
		}
	}
}
