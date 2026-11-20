package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/tournament/internal/orchestrator"
	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	"github.com/bvivg/axon/core/services/tournament/internal/store"
	tournamentv1 "github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1/tournamentv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

const (
	DefaultListLimit = 30
	MaxListLimit     = 100
	AdminRole        = "admin"
)

type Tournaments interface {
	Create(ctx context.Context, creator, request uuid.UUID, spec saga.Spec) (saga.Tournament, error)
	Get(ctx context.Context, id uuid.UUID) (saga.Tournament, error)
	List(ctx context.Context, status saga.Status, limit int) ([]store.Summary, error)
	Register(ctx context.Context, id, user uuid.UUID) (saga.Tournament, error)
	Withdraw(ctx context.Context, id, user uuid.UUID) (saga.Tournament, error)
	Start(ctx context.Context, id uuid.UUID) (saga.Tournament, error)
	Cancel(ctx context.Context, id uuid.UUID, admin bool) (saga.Tournament, error)
	NoShow() time.Duration
}

type Verifier interface {
	Verify(raw string) (authn.Claims, error)
}

type Roles interface {
	Roles(ctx context.Context, authorization string) ([]string, error)
}

type Handler struct {
	tournaments Tournaments
	verifier    Verifier
	roles       Roles
	games       []string
	log         *slog.Logger
}

var _ tournamentv1connect.TournamentServiceHandler = (*Handler)(nil)

func New(tournaments Tournaments, verifier Verifier, roles Roles, games []string, log *slog.Logger) *Handler {
	return &Handler{tournaments: tournaments, verifier: verifier, roles: roles, games: games, log: log}
}

var (
	errSignIn      = errors.New("sign in to take part in tournaments")
	errTournament  = errors.New("tournament_id must be a UUID")
	errRequest     = errors.New("request_id must be a UUID")
	errGame        = errors.New("unknown game")
	errNotFound    = errors.New("tournament not found")
	errUnavailable = errors.New("tournaments are unavailable")
	errManage      = errors.New("only the creator or an admin can do this")
	errAdminOnly   = errors.New("only an admin can cancel a tournament that has started")
)

func (h *Handler) caller(headers http.Header) (uuid.UUID, error) {
	raw, err := authn.BearerToken(headers)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeUnauthenticated, errSignIn)
	}
	claims, err := h.verifier.Verify(raw)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeUnauthenticated, errSignIn)
	}
	return claims.UserID, nil
}

func tournamentID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, connect.NewError(connect.CodeInvalidArgument, errTournament)
	}
	return id, nil
}

func (h *Handler) fail(ctx context.Context, op string, err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, errNotFound)
	case errors.Is(err, saga.ErrInvalid):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, orchestrator.ErrAdminOnly):
		return connect.NewError(connect.CodePermissionDenied, errAdminOnly)
	case errors.Is(err, saga.ErrClosed), errors.Is(err, saga.ErrFull), errors.Is(err, saga.ErrTooFew), errors.Is(err, saga.ErrFinal):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, errUnavailable)
	}
	h.log.ErrorContext(ctx, "tournament request failed", "op", op, "error", err)
	return connect.NewError(connect.CodeUnavailable, errUnavailable)
}

var formats = map[tournamentv1.TournamentFormat]saga.Format{
	tournamentv1.TournamentFormat_TOURNAMENT_FORMAT_SINGLE_ELIMINATION: saga.SingleElimination,
	tournamentv1.TournamentFormat_TOURNAMENT_FORMAT_SWISS:              saga.Swiss,
}

func (h *Handler) CreateTournament(ctx context.Context, req *connect.Request[tournamentv1.CreateTournamentRequest]) (*connect.Response[tournamentv1.CreateTournamentResponse], error) {
	user, err := h.caller(req.Header())
	if err != nil {
		return nil, err
	}
	request, err := uuid.Parse(req.Msg.GetRequestId())
	if err != nil || request == uuid.Nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errRequest)
	}
	if !slices.Contains(h.games, req.Msg.GetGame()) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errGame)
	}
	format, ok := formats[req.Msg.GetFormat()]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown tournament format"))
	}
	spec := saga.Spec{Name: req.Msg.GetName(), Game: req.Msg.GetGame(), Format: format, Capacity: int(req.Msg.GetCapacity()), Rounds: int(req.Msg.GetRounds())}
	t, err := h.tournaments.Create(ctx, user, request, spec)
	if err != nil {
		return nil, h.fail(ctx, "create", err)
	}
	return connect.NewResponse(&tournamentv1.CreateTournamentResponse{Tournament: Present(t, h.tournaments.NoShow())}), nil
}

func (h *Handler) RegisterForTournament(ctx context.Context, req *connect.Request[tournamentv1.RegisterForTournamentRequest]) (*connect.Response[tournamentv1.RegisterForTournamentResponse], error) {
	user, err := h.caller(req.Header())
	if err != nil {
		return nil, err
	}
	id, err := tournamentID(req.Msg.GetTournamentId())
	if err != nil {
		return nil, err
	}
	t, err := h.tournaments.Register(ctx, id, user)
	if err != nil {
		return nil, h.fail(ctx, "register", err)
	}
	return connect.NewResponse(&tournamentv1.RegisterForTournamentResponse{Tournament: Present(t, h.tournaments.NoShow())}), nil
}

func (h *Handler) WithdrawFromTournament(ctx context.Context, req *connect.Request[tournamentv1.WithdrawFromTournamentRequest]) (*connect.Response[tournamentv1.WithdrawFromTournamentResponse], error) {
	user, err := h.caller(req.Header())
	if err != nil {
		return nil, err
	}
	id, err := tournamentID(req.Msg.GetTournamentId())
	if err != nil {
		return nil, err
	}
	t, err := h.tournaments.Withdraw(ctx, id, user)
	if err != nil {
		return nil, h.fail(ctx, "withdraw", err)
	}
	return connect.NewResponse(&tournamentv1.WithdrawFromTournamentResponse{Tournament: Present(t, h.tournaments.NoShow())}), nil
}

func (h *Handler) admin(ctx context.Context, headers http.Header) (bool, error) {
	roles, err := h.roles.Roles(ctx, headers.Get(authn.Header))
	if err != nil {
		h.log.WarnContext(ctx, "role lookup failed", "error", err)
		return false, connect.NewError(connect.CodeUnavailable, errUnavailable)
	}
	return slices.Contains(roles, AdminRole), nil
}

func (h *Handler) manager(ctx context.Context, headers http.Header, id uuid.UUID, cancelling bool) (bool, error) {
	user, err := h.caller(headers)
	if err != nil {
		return false, err
	}
	current, err := h.tournaments.Get(ctx, id)
	if err != nil {
		return false, h.fail(ctx, "get", err)
	}
	admin := false
	if current.Creator != user || cancelling && current.Started() && !current.Over() {
		if admin, err = h.admin(ctx, headers); err != nil {
			return false, err
		}
	}
	if current.Creator != user && !admin {
		return false, connect.NewError(connect.CodePermissionDenied, errManage)
	}
	return admin, nil
}

func (h *Handler) StartTournament(ctx context.Context, req *connect.Request[tournamentv1.StartTournamentRequest]) (*connect.Response[tournamentv1.StartTournamentResponse], error) {
	id, err := tournamentID(req.Msg.GetTournamentId())
	if err != nil {
		return nil, err
	}
	if _, err := h.manager(ctx, req.Header(), id, false); err != nil {
		return nil, err
	}
	t, err := h.tournaments.Start(ctx, id)
	if err != nil {
		return nil, h.fail(ctx, "start", err)
	}
	return connect.NewResponse(&tournamentv1.StartTournamentResponse{Tournament: Present(t, h.tournaments.NoShow())}), nil
}

func (h *Handler) CancelTournament(ctx context.Context, req *connect.Request[tournamentv1.CancelTournamentRequest]) (*connect.Response[tournamentv1.CancelTournamentResponse], error) {
	id, err := tournamentID(req.Msg.GetTournamentId())
	if err != nil {
		return nil, err
	}
	admin, err := h.manager(ctx, req.Header(), id, true)
	if err != nil {
		return nil, err
	}
	t, err := h.tournaments.Cancel(ctx, id, admin)
	if err != nil {
		return nil, h.fail(ctx, "cancel", err)
	}
	return connect.NewResponse(&tournamentv1.CancelTournamentResponse{Tournament: Present(t, h.tournaments.NoShow())}), nil
}

func (h *Handler) GetTournament(ctx context.Context, req *connect.Request[tournamentv1.GetTournamentRequest]) (*connect.Response[tournamentv1.GetTournamentResponse], error) {
	if _, err := h.caller(req.Header()); err != nil {
		return nil, err
	}
	id, err := tournamentID(req.Msg.GetTournamentId())
	if err != nil {
		return nil, err
	}
	t, err := h.tournaments.Get(ctx, id)
	if err != nil {
		return nil, h.fail(ctx, "get", err)
	}
	return connect.NewResponse(&tournamentv1.GetTournamentResponse{Tournament: Present(t, h.tournaments.NoShow())}), nil
}

var statuses = map[tournamentv1.TournamentStatus]saga.Status{
	tournamentv1.TournamentStatus_TOURNAMENT_STATUS_REGISTRATION: saga.Registration,
	tournamentv1.TournamentStatus_TOURNAMENT_STATUS_SEEDING:      saga.Seeding,
	tournamentv1.TournamentStatus_TOURNAMENT_STATUS_RUNNING:      saga.Running,
	tournamentv1.TournamentStatus_TOURNAMENT_STATUS_FINISHED:     saga.Finished,
	tournamentv1.TournamentStatus_TOURNAMENT_STATUS_CANCELLED:    saga.Cancelled,
}

func (h *Handler) ListTournaments(ctx context.Context, req *connect.Request[tournamentv1.ListTournamentsRequest]) (*connect.Response[tournamentv1.ListTournamentsResponse], error) {
	if _, err := h.caller(req.Header()); err != nil {
		return nil, err
	}
	var status saga.Status
	if req.Msg.Status != nil {
		known, ok := statuses[req.Msg.GetStatus()]
		if !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown tournament status"))
		}
		status = known
	}
	limit := int(req.Msg.GetLimit())
	if limit <= 0 {
		limit = DefaultListLimit
	}
	summaries, err := h.tournaments.List(ctx, status, min(limit, MaxListLimit))
	if err != nil {
		return nil, h.fail(ctx, "list", err)
	}
	out := &tournamentv1.ListTournamentsResponse{Tournaments: make([]*tournamentv1.Tournament, 0, len(summaries))}
	for _, s := range summaries {
		item := Present(s.Tournament, h.tournaments.NoShow())
		item.PlayerCount = uint32(s.Players)
		out.Tournaments = append(out.Tournaments, item)
	}
	return connect.NewResponse(out), nil
}
