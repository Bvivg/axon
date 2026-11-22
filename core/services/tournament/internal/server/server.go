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
	"github.com/bvivg/axon/core/shared/pkg/authz"
)

const (
	DefaultListLimit = 30
	MaxListLimit     = 100
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

type Moderators interface {
	Moderator(ctx context.Context, user uuid.UUID, authorization string) (bool, error)
}

type Handler struct {
	tournaments Tournaments
	verifier    Verifier
	moderators  Moderators
	games       []string
	log         *slog.Logger
	recorder    authz.Recorder
}

var _ tournamentv1connect.TournamentServiceHandler = (*Handler)(nil)

func New(tournaments Tournaments, verifier Verifier, moderators Moderators, games []string, log *slog.Logger) *Handler {
	return &Handler{tournaments: tournaments, verifier: verifier, moderators: moderators, games: games, log: log}
}

func (h *Handler) WithAudit(record authz.Recorder) *Handler {
	h.recorder = record
	return h
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

func (h *Handler) admin(ctx context.Context, user uuid.UUID, headers http.Header) (bool, error) {
	moderator, err := h.moderators.Moderator(ctx, user, headers.Get(authn.Header))
	if err != nil {
		h.log.WarnContext(ctx, "permission lookup failed", "error", err)
		return false, connect.NewError(connect.CodeUnavailable, errUnavailable)
	}
	return moderator, nil
}

type management struct {
	user     uuid.UUID
	admin    bool
	override bool
	game     string
}

func (h *Handler) manager(ctx context.Context, headers http.Header, id uuid.UUID, cancelling bool, name, reason string) (management, error) {
	user, err := h.caller(headers)
	if err != nil {
		return management{}, err
	}
	current, err := h.tournaments.Get(ctx, id)
	if err != nil {
		return management{}, h.fail(ctx, "get", err)
	}
	m := management{user: user, game: current.Spec.Game}
	if current.Creator != user || cancelling && current.Started() && !current.Over() {
		if m.admin, err = h.admin(ctx, user, headers); err != nil {
			return management{}, err
		}
	}
	if current.Creator != user && !m.admin {
		h.record(ctx, headers, m, name, id, reason, authz.Denied)
		return management{}, connect.NewError(connect.CodePermissionDenied, errManage)
	}
	m.override = m.admin && (current.Creator != user || cancelling && current.Started() && !current.Over())
	if m.override && reason == "" {
		_, err := authz.Reason(reason)
		return management{}, err
	}
	return m, nil
}

func optionalReason(raw *string) (string, error) {
	if raw == nil {
		return "", nil
	}
	return authz.Reason(*raw)
}

func (h *Handler) audit(ctx context.Context, headers http.Header, m management, name string, id uuid.UUID, reason string) {
	if m.override {
		h.record(ctx, headers, m, name, id, reason, authz.Succeeded)
	}
}

func (h *Handler) record(ctx context.Context, headers http.Header, m management, name string, id uuid.UUID, reason string, outcome authz.Outcome) {
	if h.recorder == nil {
		return
	}
	action := authz.NewAction(ctx, "tournament", name, "tournament", id.String(), reason)
	action.Outcome = outcome
	action.ActorID = m.user
	action.ClientIP = headers.Get(authn.ClientIPHeader)
	action.UserAgent = headers.Get("User-Agent")
	action.Details = map[string]string{"game": m.game}
	if err := h.recorder(context.WithoutCancel(ctx), action); err != nil {
		h.log.ErrorContext(ctx, "could not record a tournament moderation", "tournament_id", id, "error", err)
	}
}

func (h *Handler) StartTournament(ctx context.Context, req *connect.Request[tournamentv1.StartTournamentRequest]) (*connect.Response[tournamentv1.StartTournamentResponse], error) {
	id, err := tournamentID(req.Msg.GetTournamentId())
	if err != nil {
		return nil, err
	}
	reason, err := optionalReason(req.Msg.Reason)
	if err != nil {
		return nil, err
	}
	m, err := h.manager(ctx, req.Header(), id, false, "tournament_started", reason)
	if err != nil {
		return nil, err
	}
	t, err := h.tournaments.Start(ctx, id)
	if err != nil {
		return nil, h.fail(ctx, "start", err)
	}
	h.audit(ctx, req.Header(), m, "tournament_started", id, reason)
	return connect.NewResponse(&tournamentv1.StartTournamentResponse{Tournament: Present(t, h.tournaments.NoShow())}), nil
}

func (h *Handler) CancelTournament(ctx context.Context, req *connect.Request[tournamentv1.CancelTournamentRequest]) (*connect.Response[tournamentv1.CancelTournamentResponse], error) {
	id, err := tournamentID(req.Msg.GetTournamentId())
	if err != nil {
		return nil, err
	}
	reason, err := optionalReason(req.Msg.Reason)
	if err != nil {
		return nil, err
	}
	m, err := h.manager(ctx, req.Header(), id, true, "tournament_cancelled", reason)
	if err != nil {
		return nil, err
	}
	t, err := h.tournaments.Cancel(ctx, id, m.admin)
	if err != nil {
		return nil, h.fail(ctx, "cancel", err)
	}
	h.audit(ctx, req.Header(), m, "tournament_cancelled", id, reason)
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
