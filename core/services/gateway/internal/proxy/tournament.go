package proxy

import (
	"context"

	"connectrpc.com/connect"

	tournamentv1 "github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1/tournamentv1connect"
)

type Tournament struct {
	client tournamentv1connect.TournamentServiceClient
}

var _ tournamentv1connect.TournamentServiceHandler = (*Tournament)(nil)

func NewTournament(client connect.HTTPClient, url string, opts ...connect.ClientOption) *Tournament {
	return &Tournament{client: tournamentv1connect.NewTournamentServiceClient(client, url, opts...)}
}

func (p *Tournament) CreateTournament(ctx context.Context, req *connect.Request[tournamentv1.CreateTournamentRequest]) (*connect.Response[tournamentv1.CreateTournamentResponse], error) {
	return p.client.CreateTournament(ctx, forward(ctx, req))
}

func (p *Tournament) RegisterForTournament(ctx context.Context, req *connect.Request[tournamentv1.RegisterForTournamentRequest]) (*connect.Response[tournamentv1.RegisterForTournamentResponse], error) {
	return p.client.RegisterForTournament(ctx, forward(ctx, req))
}

func (p *Tournament) WithdrawFromTournament(ctx context.Context, req *connect.Request[tournamentv1.WithdrawFromTournamentRequest]) (*connect.Response[tournamentv1.WithdrawFromTournamentResponse], error) {
	return p.client.WithdrawFromTournament(ctx, forward(ctx, req))
}

func (p *Tournament) StartTournament(ctx context.Context, req *connect.Request[tournamentv1.StartTournamentRequest]) (*connect.Response[tournamentv1.StartTournamentResponse], error) {
	return p.client.StartTournament(ctx, forward(ctx, req))
}

func (p *Tournament) GetTournament(ctx context.Context, req *connect.Request[tournamentv1.GetTournamentRequest]) (*connect.Response[tournamentv1.GetTournamentResponse], error) {
	return p.client.GetTournament(ctx, forward(ctx, req))
}

func (p *Tournament) ListTournaments(ctx context.Context, req *connect.Request[tournamentv1.ListTournamentsRequest]) (*connect.Response[tournamentv1.ListTournamentsResponse], error) {
	return p.client.ListTournaments(ctx, forward(ctx, req))
}

func (p *Tournament) CancelTournament(ctx context.Context, req *connect.Request[tournamentv1.CancelTournamentRequest]) (*connect.Response[tournamentv1.CancelTournamentResponse], error) {
	return p.client.CancelTournament(ctx, forward(ctx, req))
}
