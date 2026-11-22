package server

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/tournament/internal/orchestrator"
	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	"github.com/bvivg/axon/core/services/tournament/internal/store"
	tournamentv1 "github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1/tournamentv1connect"
	"github.com/bvivg/axon/core/shared/pkg/admincrud"
	"github.com/bvivg/axon/core/shared/pkg/authz"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

var AdminRules = authz.Rules{
	tournamentv1connect.TournamentAdminServiceUpdateTournamentProcedure:       authz.GamesModerate,
	tournamentv1connect.TournamentAdminServiceRemoveTournamentPlayerProcedure: authz.GamesModerate,
}

var tournamentFields = admincrud.Fields{
	"name":     authz.GamesModerate,
	"capacity": authz.GamesModerate,
	"rounds":   authz.GamesModerate,
}

var diffPaths = map[string]string{"name": "name", "capacity": "capacity", "rounds": "total_rounds"}

type AdminTournaments interface {
	Revise(ctx context.Context, id uuid.UUID, version int64, revise func(saga.Spec) saga.Spec, audit orchestrator.Audit) (saga.Tournament, saga.Tournament, error)
	Remove(ctx context.Context, id, user uuid.UUID, audit orchestrator.Audit) (saga.Tournament, error)
	NoShow() time.Duration
}

type Admin struct {
	tournaments AdminTournaments
	log         *slog.Logger
}

var _ tournamentv1connect.TournamentAdminServiceHandler = (*Admin)(nil)

func NewAdmin(tournaments AdminTournaments, log *slog.Logger) *Admin {
	return &Admin{tournaments: tournaments, log: log}
}

var (
	errClosedForChanges = errors.New("only a tournament that is still open for registration can be changed")
	errNotRegistered    = errors.New("this player is not registered for the tournament")
)

func (a *Admin) fail(ctx context.Context, op string, err error) error {
	var spec *saga.SpecError
	var stale *orchestrator.StaleError
	switch {
	case errors.As(err, &spec):
		return admincrud.Invalid(spec.Field, "valid", spec.Reason)
	case errors.As(err, &stale):
		return admincrud.Conflict(stale.Current)
	case errors.Is(err, store.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, errNotFound)
	case errors.Is(err, saga.ErrClosed):
		return connect.NewError(connect.CodeFailedPrecondition, errClosedForChanges)
	case errors.Is(err, saga.ErrNotRegistered):
		return connect.NewError(connect.CodeNotFound, errNotRegistered)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, errUnavailable)
	}
	a.log.ErrorContext(ctx, "tournament admin request failed", "op", op, "error", err)
	return connect.NewError(connect.CodeUnavailable, errUnavailable)
}

func (a *Admin) audit(ctx context.Context, name, reason string, paths []string, details map[string]string) orchestrator.Audit {
	return func(before, after saga.Tournament) outbox.Event {
		action := authz.NewAction(ctx, "tournament", name, "tournament", after.ID.String(), reason)
		action.Details = map[string]string{"game": after.Spec.Game}
		for key, value := range details {
			action.Details[key] = value
		}
		if len(paths) > 0 {
			action.Changes = admincrud.Diff(Present(before, 0), Present(after, 0), paths)
		}
		return action.Event(uuid.New())
	}
}

func (a *Admin) UpdateTournament(ctx context.Context, req *connect.Request[tournamentv1.UpdateTournamentRequest]) (*connect.Response[tournamentv1.UpdateTournamentResponse], error) {
	actor, ok := authz.ActorFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errSignIn)
	}
	id, err := tournamentID(req.Msg.GetTournamentId())
	if err != nil {
		return nil, err
	}
	version, err := admincrud.ParseEtag(req.Msg.GetEtag())
	if err != nil {
		return nil, err
	}
	paths, err := tournamentFields.Paths(req.Msg.GetUpdateMask(), actor.Permissions)
	if err != nil {
		return nil, err
	}
	reason, err := authz.Reason(req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	changed := make([]string, len(paths))
	for i, path := range paths {
		changed[i] = diffPaths[path]
	}
	revise := func(spec saga.Spec) saga.Spec {
		for _, path := range paths {
			switch path {
			case "name":
				spec.Name = req.Msg.GetName()
			case "capacity":
				spec.Capacity = int(req.Msg.GetCapacity())
			case "rounds":
				spec.Rounds = int(req.Msg.GetRounds())
			}
		}
		return spec
	}
	_, after, err := a.tournaments.Revise(ctx, id, version, revise, a.audit(ctx, "tournament_updated", reason, changed, nil))
	if err != nil {
		return nil, a.fail(ctx, "update", err)
	}
	return connect.NewResponse(&tournamentv1.UpdateTournamentResponse{Tournament: Present(after, a.tournaments.NoShow())}), nil
}

func (a *Admin) RemoveTournamentPlayer(ctx context.Context, req *connect.Request[tournamentv1.RemoveTournamentPlayerRequest]) (*connect.Response[tournamentv1.RemoveTournamentPlayerResponse], error) {
	id, err := tournamentID(req.Msg.GetTournamentId())
	if err != nil {
		return nil, err
	}
	user, err := uuid.Parse(req.Msg.GetUserId())
	if err != nil || user == uuid.Nil {
		return nil, admincrud.Invalid("user_id", "uuid", "user_id is not valid")
	}
	reason, err := authz.Reason(req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	t, err := a.tournaments.Remove(ctx, id, user, a.audit(ctx, "tournament_player_removed", reason, nil, map[string]string{"user_id": user.String()}))
	if err != nil {
		return nil, a.fail(ctx, "remove player", err)
	}
	return connect.NewResponse(&tournamentv1.RemoveTournamentPlayerResponse{Tournament: Present(t, a.tournaments.NoShow())}), nil
}
