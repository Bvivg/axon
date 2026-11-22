package server_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	validatepb "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/bvivg/axon/core/services/tournament/internal/orchestrator"
	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	"github.com/bvivg/axon/core/services/tournament/internal/server"
	"github.com/bvivg/axon/core/services/tournament/internal/store"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	tournamentv1 "github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1"
	"github.com/bvivg/axon/core/shared/pkg/authz"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

type adminFake struct {
	current saga.Tournament
	fail    error
	events  []outbox.Event
}

func (f *adminFake) Revise(_ context.Context, id uuid.UUID, version int64, revise func(saga.Spec) saga.Spec, audit orchestrator.Audit) (saga.Tournament, saga.Tournament, error) {
	if f.fail != nil {
		return saga.Tournament{}, saga.Tournament{}, f.fail
	}
	if id != f.current.ID {
		return saga.Tournament{}, saga.Tournament{}, store.ErrNotFound
	}
	next, changed, err := f.current.Revise(revise(f.current.Spec))
	if err != nil || !changed {
		return f.current, f.current, err
	}
	if version != f.current.Version {
		return saga.Tournament{}, saga.Tournament{}, &orchestrator.StaleError{Current: f.current.Version}
	}
	next.Version++
	f.events = append(f.events, audit(f.current, next))
	before := f.current
	f.current = next
	return before, next, nil
}

func (f *adminFake) Remove(_ context.Context, _, user uuid.UUID, audit orchestrator.Audit) (saga.Tournament, error) {
	next, changed, err := f.current.Withdraw(user)
	if err != nil {
		return saga.Tournament{}, err
	}
	if !changed {
		return saga.Tournament{}, saga.ErrNotRegistered
	}
	f.events = append(f.events, audit(f.current, next))
	f.current = next
	return next, nil
}

func (f *adminFake) NoShow() time.Duration { return time.Minute }

func moderator(granted ...authz.Permission) context.Context {
	names := make([]string, len(granted))
	for i, p := range granted {
		names[i] = p.String()
	}
	return authz.WithActor(context.Background(), authz.Actor{UserID: admin, Permissions: authz.NewSet(names)})
}

func openTournament(t *testing.T) saga.Tournament {
	t.Helper()
	tour, err := saga.Create(uuid.New(), creator, saga.Spec{Name: "League", Game: "chess", Format: saga.Swiss, Capacity: 8, Rounds: 3}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tour.Version = 4
	for _, user := range []uuid.UUID{creator, stranger} {
		if tour, _, err = tour.Register(user, 1500, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return tour
}

func update(id string, etag string, paths ...string) *tournamentv1.UpdateTournamentRequest {
	return &tournamentv1.UpdateTournamentRequest{TournamentId: id, Name: "Winter league", Capacity: 2, Rounds: 5,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: paths}, Etag: etag, Reason: "typo in the name"}
}

func errorDetail[T any](t *testing.T, err error) T {
	t.Helper()
	var ce *connect.Error
	if !errors.As(err, &ce) {
		t.Fatalf("not a connect error: %v", err)
	}
	for _, d := range ce.Details() {
		if value, derr := d.Value(); derr == nil {
			if typed, ok := value.(T); ok {
				return typed
			}
		}
	}
	var zero T
	t.Fatalf("no %T detail in %v", zero, err)
	return zero
}

func TestUpdateTournament(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tt := range []struct {
		name  string
		ctx   context.Context
		req   func(id string) *tournamentv1.UpdateTournamentRequest
		fail  error
		code  connect.Code
		field string
	}{
		{"needs an actor", context.Background(), func(id string) *tournamentv1.UpdateTournamentRequest { return update(id, `W/"4"`, "name") }, nil, connect.CodeUnauthenticated, ""},
		{"bad etag", moderator(authz.GamesModerate), func(id string) *tournamentv1.UpdateTournamentRequest { return update(id, "latest", "name") }, nil, connect.CodeInvalidArgument, "etag"},
		{"unknown path", moderator(authz.GamesModerate), func(id string) *tournamentv1.UpdateTournamentRequest { return update(id, `W/"4"`, "game") }, nil, connect.CodeInvalidArgument, "update_mask"},
		{"stale etag", moderator(authz.GamesModerate), func(id string) *tournamentv1.UpdateTournamentRequest { return update(id, `W/"3"`, "name") }, nil, connect.CodeAborted, ""},
		{"capacity below registered", moderator(authz.GamesModerate), func(id string) *tournamentv1.UpdateTournamentRequest { return update(id, `W/"4"`, "capacity") }, nil, connect.CodeInvalidArgument, "capacity"},
		{"closed", moderator(authz.GamesModerate), func(id string) *tournamentv1.UpdateTournamentRequest { return update(id, `W/"4"`, "name") }, saga.ErrClosed, connect.CodeFailedPrecondition, ""},
		{"missing", moderator(authz.GamesModerate), func(string) *tournamentv1.UpdateTournamentRequest { return update(uuid.NewString(), `W/"4"`, "name") }, nil, connect.CodeNotFound, ""},
		{"unexpected", moderator(authz.GamesModerate), func(id string) *tournamentv1.UpdateTournamentRequest { return update(id, `W/"4"`, "name") }, errors.New("boom"), connect.CodeUnavailable, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &adminFake{current: openTournament(t), fail: tt.fail}
			_, err := server.NewAdmin(f, quiet).UpdateTournament(tt.ctx, connect.NewRequest(tt.req(f.current.ID.String())))
			if connect.CodeOf(err) != tt.code {
				t.Fatalf("code = %v (%v), want %v", connect.CodeOf(err), err, tt.code)
			}
			if tt.field != "" {
				violations := errorDetail[*validatepb.Violations](t, err)
				if got := violations.GetViolations()[0].GetField().GetElements()[0].GetFieldName(); got != tt.field {
					t.Fatalf("field = %q, want %q", got, tt.field)
				}
			}
			if tt.code == connect.CodeAborted {
				if info := errorDetail[*errdetails.ErrorInfo](t, err); info.GetMetadata()["etag"] != `W/"4"` {
					t.Fatalf("conflict info = %v", info)
				}
			}
			if len(f.events) != 0 {
				t.Fatalf("failed update recorded %d events", len(f.events))
			}
		})
	}
}

func TestUpdateTournamentChangesOnlyTheMaskedFieldsAndAuditsTheDiff(t *testing.T) {
	f := &adminFake{current: openTournament(t)}
	svc := server.NewAdmin(f, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := svc.UpdateTournament(moderator(authz.GamesModerate), connect.NewRequest(update(f.current.ID.String(), `W/"4"`, "name", "rounds")))
	if err != nil {
		t.Fatal(err)
	}
	got := res.Msg.GetTournament()
	if got.GetName() != "Winter league" || got.GetCapacity() != 8 || got.GetTotalRounds() != 5 || got.GetEtag() != `W/"5"` {
		t.Fatalf("updated = %q cap %d rounds %d etag %s", got.GetName(), got.GetCapacity(), got.GetTotalRounds(), got.GetEtag())
	}
	if len(f.events) != 1 {
		t.Fatalf("events = %d", len(f.events))
	}
	action := f.events[0].Payload.(*eventsv1.AdminAction)
	if action.GetAction() != "tournament_updated" || action.GetActorId() != admin.String() || action.GetDetails()["game"] != "chess" ||
		action.GetReason() != "typo in the name" {
		t.Fatalf("action = %v", action)
	}
	changes := map[string][2]string{}
	for _, c := range action.GetChanges() {
		changes[c.GetField()] = [2]string{c.GetBefore(), c.GetAfter()}
	}
	if len(changes) != 2 || changes["name"] != [2]string{"League", "Winter league"} || changes["total_rounds"] != [2]string{"3", "5"} {
		t.Fatalf("changes = %v", changes)
	}
	again, err := svc.UpdateTournament(moderator(authz.GamesModerate), connect.NewRequest(update(f.current.ID.String(), `W/"4"`, "name", "rounds")))
	if err != nil || again.Msg.GetTournament().GetEtag() != `W/"5"` || len(f.events) != 1 {
		t.Fatalf("replay = %v, %v, events %d", again, err, len(f.events))
	}
}

func TestRemoveTournamentPlayer(t *testing.T) {
	f := &adminFake{current: openTournament(t)}
	svc := server.NewAdmin(f, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := &tournamentv1.RemoveTournamentPlayerRequest{TournamentId: f.current.ID.String(), UserId: stranger.String(), Reason: "left the club"}
	res, err := svc.RemoveTournamentPlayer(moderator(authz.GamesModerate), connect.NewRequest(req))
	if err != nil || res.Msg.GetTournament().GetPlayerCount() != 1 {
		t.Fatalf("remove = %v, %v", res, err)
	}
	action := f.events[0].Payload.(*eventsv1.AdminAction)
	if action.GetAction() != "tournament_player_removed" || action.GetDetails()["user_id"] != stranger.String() || action.GetTargetId() != f.current.ID.String() {
		t.Fatalf("action = %v", action)
	}
	if _, err := svc.RemoveTournamentPlayer(moderator(authz.GamesModerate), connect.NewRequest(req)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("second removal = %v", err)
	}
	req.UserId = "nobody"
	if _, err := svc.RemoveTournamentPlayer(moderator(authz.GamesModerate), connect.NewRequest(req)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad user id = %v", err)
	}
}

func TestAdminRulesCoverEveryProcedure(t *testing.T) {
	methods := tournamentv1.File_axon_tournament_v1_admin_proto.Services().ByName("TournamentAdminService").Methods()
	for i := range methods.Len() {
		procedure := "/axon.tournament.v1.TournamentAdminService/" + string(methods.Get(i).Name())
		if server.AdminRules[procedure] != authz.GamesModerate {
			t.Fatalf("%s has rule %v", procedure, server.AdminRules[procedure])
		}
	}
}
