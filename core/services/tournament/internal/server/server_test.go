package server_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/tournament/internal/orchestrator"
	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	"github.com/bvivg/axon/core/services/tournament/internal/server"
	"github.com/bvivg/axon/core/services/tournament/internal/store"
	tournamentv1 "github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

type tokens map[string]uuid.UUID

func (t tokens) Verify(raw string) (authn.Claims, error) {
	user, ok := t[raw]
	if !ok {
		return authn.Claims{}, authn.ErrInvalidToken
	}
	return authn.Claims{UserID: user}, nil
}

type roles struct {
	admins map[string]bool
	calls  int
	fail   error
}

func (r *roles) Roles(_ context.Context, authorization string) ([]string, error) {
	r.calls++
	if r.fail != nil {
		return nil, r.fail
	}
	if r.admins[authorization] {
		return []string{"user", server.AdminRole}, nil
	}
	return []string{"user"}, nil
}

type fake struct {
	t         saga.Tournament
	created   []saga.Spec
	cancelled []bool
	started   int
	err       error
}

func (f *fake) Create(_ context.Context, creator, request uuid.UUID, spec saga.Spec) (saga.Tournament, error) {
	f.created = append(f.created, spec)
	if f.err != nil {
		return saga.Tournament{}, f.err
	}
	return saga.Create(orchestrator.TournamentID(creator, request), creator, spec, time.Now())
}

func (f *fake) Get(context.Context, uuid.UUID) (saga.Tournament, error) {
	if f.t.ID == uuid.Nil {
		return saga.Tournament{}, store.ErrNotFound
	}
	return f.t, nil
}

func (f *fake) List(context.Context, saga.Status, int) ([]store.Summary, error) {
	return []store.Summary{{Tournament: f.t, Players: 3}}, nil
}

func (f *fake) Register(_ context.Context, _, user uuid.UUID) (saga.Tournament, error) {
	next, _, err := f.t.Register(user, 1500, time.Now())
	return next, err
}

func (f *fake) Withdraw(_ context.Context, _, user uuid.UUID) (saga.Tournament, error) {
	next, _, err := f.t.Withdraw(user)
	return next, err
}

func (f *fake) Start(context.Context, uuid.UUID) (saga.Tournament, error) {
	f.started++
	return f.t, f.err
}

func (f *fake) Cancel(_ context.Context, _ uuid.UUID, admin bool) (saga.Tournament, error) {
	f.cancelled = append(f.cancelled, admin)
	if f.t.Started() && !admin {
		return saga.Tournament{}, orchestrator.ErrAdminOnly
	}
	return f.t, nil
}

func (f *fake) NoShow() time.Duration { return time.Minute }

var (
	creator  = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	stranger = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	admin    = uuid.MustParse("33333333-3333-4333-8333-333333333333")
	users    = tokens{"creator": creator, "stranger": stranger, "admin": admin}
)

func handler(t *testing.T, f *fake) (*server.Handler, *roles) {
	t.Helper()
	r := &roles{admins: map[string]bool{"Bearer admin": true}}
	return server.New(f, users, r, []string{"tictactoe", "chess"}, slog.New(slog.NewTextHandler(io.Discard, nil))), r
}

func as[T any](token string, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	if token != "" {
		req.Header().Set("Authorization", "Bearer "+token)
	}
	return req
}

func tournament(t *testing.T, started bool) saga.Tournament {
	t.Helper()
	tour, err := saga.Create(uuid.New(), creator, saga.Spec{Name: "Cup", Game: "tictactoe", Format: saga.SingleElimination, Capacity: 4}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if started {
		tour, _, _ = tour.Register(creator, 1500, time.Now())
		tour, _, _ = tour.Register(stranger, 1500, time.Now())
		if tour, _, err = tour.Start(time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return tour
}

func TestCreateValidatesTheRequest(t *testing.T) {
	f := &fake{}
	h, _ := handler(t, f)
	ctx := context.Background()
	good := &tournamentv1.CreateTournamentRequest{RequestId: uuid.NewString(), Name: "Cup", Game: "tictactoe",
		Format: tournamentv1.TournamentFormat_TOURNAMENT_FORMAT_SINGLE_ELIMINATION, Capacity: 8}
	res, err := h.CreateTournament(ctx, as("stranger", good))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Msg.GetTournament(); got.GetCreatorId() != stranger.String() || got.GetStatus() != tournamentv1.TournamentStatus_TOURNAMENT_STATUS_REGISTRATION ||
		got.GetFormat() != tournamentv1.TournamentFormat_TOURNAMENT_FORMAT_SINGLE_ELIMINATION || got.GetNoShowSeconds() != 60 {
		t.Fatalf("created = %v", got)
	}
	for name, tt := range map[string]struct {
		token string
		edit  func(*tournamentv1.CreateTournamentRequest)
		code  connect.Code
	}{
		"anonymous":      {"", func(*tournamentv1.CreateTournamentRequest) {}, connect.CodeUnauthenticated},
		"bad token":      {"nobody", func(*tournamentv1.CreateTournamentRequest) {}, connect.CodeUnauthenticated},
		"no request id":  {"stranger", func(r *tournamentv1.CreateTournamentRequest) { r.RequestId = "" }, connect.CodeInvalidArgument},
		"unknown game":   {"stranger", func(r *tournamentv1.CreateTournamentRequest) { r.Game = "durak" }, connect.CodeInvalidArgument},
		"no format":      {"stranger", func(r *tournamentv1.CreateTournamentRequest) { r.Format = 0 }, connect.CodeInvalidArgument},
		"bracket of six": {"stranger", func(r *tournamentv1.CreateTournamentRequest) { r.Capacity = 6 }, connect.CodeInvalidArgument},
		"swiss no rounds": {"stranger", func(r *tournamentv1.CreateTournamentRequest) {
			r.Format = tournamentv1.TournamentFormat_TOURNAMENT_FORMAT_SWISS
		}, connect.CodeInvalidArgument},
		"name too long":    {"stranger", func(r *tournamentv1.CreateTournamentRequest) { r.Name = string(make([]byte, 65)) + "x" }, connect.CodeInvalidArgument},
		"store is offline": {"stranger", func(*tournamentv1.CreateTournamentRequest) { f.err = errors.New("connection refused") }, connect.CodeUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			f.err = nil
			req := &tournamentv1.CreateTournamentRequest{RequestId: good.RequestId, Name: good.Name, Game: good.Game, Format: good.Format, Capacity: good.Capacity}
			tt.edit(req)
			if _, err := h.CreateTournament(ctx, as(tt.token, req)); connect.CodeOf(err) != tt.code {
				t.Fatalf("code = %v (%v), want %v", connect.CodeOf(err), err, tt.code)
			}
		})
	}
}

func TestOnlyTheCreatorOrAnAdminStarts(t *testing.T) {
	ctx := context.Background()
	f := &fake{t: tournament(t, false)}
	h, r := handler(t, f)
	req := &tournamentv1.StartTournamentRequest{TournamentId: f.t.ID.String()}
	if _, err := h.StartTournament(ctx, as("stranger", req)); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("stranger start = %v", err)
	}
	if _, err := h.StartTournament(ctx, as("creator", req)); err != nil {
		t.Fatal(err)
	}
	if r.calls != 1 {
		t.Fatalf("the creator's start looked up roles: %d calls", r.calls)
	}
	if _, err := h.StartTournament(ctx, as("admin", req)); err != nil {
		t.Fatal(err)
	}
	if f.started != 2 {
		t.Fatalf("started %d times", f.started)
	}
	r.fail = errors.New("auth is down")
	if _, err := h.StartTournament(ctx, as("admin", req)); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("start with auth down = %v", err)
	}
	if _, err := h.StartTournament(ctx, as("creator", &tournamentv1.StartTournamentRequest{TournamentId: "x"})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad id = %v", err)
	}
	missing := &fake{}
	h, _ = handler(t, missing)
	if _, err := h.StartTournament(ctx, as("creator", req)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("missing tournament = %v", err)
	}
}

func TestCancellingAStartedTournamentIsAdminOnly(t *testing.T) {
	ctx := context.Background()
	f := &fake{t: tournament(t, false)}
	h, _ := handler(t, f)
	req := &tournamentv1.CancelTournamentRequest{TournamentId: f.t.ID.String()}
	if _, err := h.CancelTournament(ctx, as("creator", req)); err != nil {
		t.Fatalf("creator cancel before the start = %v", err)
	}
	f.t = tournament(t, true)
	req.TournamentId = f.t.ID.String()
	if _, err := h.CancelTournament(ctx, as("creator", req)); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("creator cancel during play = %v", err)
	}
	if _, err := h.CancelTournament(ctx, as("stranger", req)); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("stranger cancel = %v", err)
	}
	if _, err := h.CancelTournament(ctx, as("admin", req)); err != nil {
		t.Fatalf("admin cancel during play = %v", err)
	}
	if want := []bool{false, false, true}; len(f.cancelled) != 3 || f.cancelled[0] != want[0] || f.cancelled[1] != want[1] || f.cancelled[2] != want[2] {
		t.Fatalf("cancel calls = %v", f.cancelled)
	}
}

func TestRegistrationAndReads(t *testing.T) {
	ctx := context.Background()
	f := &fake{t: tournament(t, false)}
	h, _ := handler(t, f)
	id := f.t.ID.String()
	res, err := h.RegisterForTournament(ctx, as("stranger", &tournamentv1.RegisterForTournamentRequest{TournamentId: id}))
	if err != nil || len(res.Msg.GetTournament().GetRegistrations()) != 1 || res.Msg.GetTournament().GetRegistrations()[0].GetUserId() != stranger.String() {
		t.Fatalf("register = %v %v", res, err)
	}
	f.t = tournament(t, true)
	if _, err := h.RegisterForTournament(ctx, as("admin", &tournamentv1.RegisterForTournamentRequest{TournamentId: id})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("late registration = %v", err)
	}
	if _, err := h.WithdrawFromTournament(ctx, as("stranger", &tournamentv1.WithdrawFromTournamentRequest{TournamentId: id})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("late withdraw = %v", err)
	}
	got, err := h.GetTournament(ctx, as("stranger", &tournamentv1.GetTournamentRequest{TournamentId: id}))
	if err != nil || got.Msg.GetTournament().GetStatus() != tournamentv1.TournamentStatus_TOURNAMENT_STATUS_SEEDING || got.Msg.GetTournament().GetPlayerCount() != 2 {
		t.Fatalf("get = %v %v", got, err)
	}
	if _, err := h.GetTournament(ctx, as("", &tournamentv1.GetTournamentRequest{TournamentId: id})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("anonymous get = %v", err)
	}
	list, err := h.ListTournaments(ctx, as("stranger", &tournamentv1.ListTournamentsRequest{Limit: 500}))
	if err != nil || len(list.Msg.GetTournaments()) != 1 || list.Msg.GetTournaments()[0].GetPlayerCount() != 3 {
		t.Fatalf("list = %v %v", list, err)
	}
	unknown := tournamentv1.TournamentStatus(42)
	if _, err := h.ListTournaments(ctx, as("stranger", &tournamentv1.ListTournamentsRequest{Status: &unknown})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("unknown status = %v", err)
	}
}

func TestPresentShowsTheBracket(t *testing.T) {
	ids := []uuid.UUID{creator, stranger, admin}
	tour, err := saga.Create(uuid.New(), creator, saga.Spec{Name: "Cup", Game: "tictactoe", Format: saga.SingleElimination, Capacity: 4}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		tour, _, _ = tour.Register(id, 1500, time.Now())
	}
	tour, _, _ = tour.Start(time.Now())
	tour, _, err = tour.Advance(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	out := server.Present(tour, time.Minute)
	if len(out.GetRounds()) != 1 || len(out.GetRounds()[0].GetMatches()) != 2 || out.GetTotalRounds() != 2 || len(out.GetStandings()) != 3 {
		t.Fatalf("presented = %v", out)
	}
	bye, game := out.GetRounds()[0].GetMatches()[0], out.GetRounds()[0].GetMatches()[1]
	if bye.GetResult() != tournamentv1.MatchResult_MATCH_RESULT_BYE || bye.SessionId != nil || len(bye.GetPlayerIds()) != 1 || bye.GetWinnerId() != creator.String() {
		t.Fatalf("bye = %v", bye)
	}
	if game.GetResult() != tournamentv1.MatchResult_MATCH_RESULT_PENDING || game.GetSessionId() == "" || len(game.GetPlayerIds()) != 2 {
		t.Fatalf("game = %v", game)
	}
	if out.GetRounds()[0].GetStatus() != tournamentv1.RoundStatus_ROUND_STATUS_RUNNING {
		t.Fatalf("round = %v", out.GetRounds()[0].GetStatus())
	}
}
