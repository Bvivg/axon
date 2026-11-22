package authz_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/authz"
)

func TestNewSetIgnoresMalformedPermissions(t *testing.T) {
	set := authz.NewSet([]string{"admin.users:read", "broken", ":read", "admin.chat:"})
	if !set.Has(authz.UsersRead) {
		t.Fatal("admin.users:read is missing")
	}
	if len(set) != 1 {
		t.Fatalf("set = %v, want only admin.users:read", set)
	}
	if set.Any(authz.UsersMutate, authz.ChatRead) {
		t.Fatal("Any matched a permission that was not granted")
	}
}

func TestCacheReusesDecisionUntilTTL(t *testing.T) {
	var calls atomic.Int32
	source := authz.SourceFunc(func(context.Context, uuid.UUID, string) ([]string, error) {
		calls.Add(1)
		return []string{"admin.games:read"}, nil
	})
	cache := authz.NewCache(source, 50*time.Millisecond)
	user := uuid.New()

	for range 3 {
		set, err := cache.Permissions(context.Background(), user, "Bearer x")
		if err != nil || !set.Has(authz.GamesRead) {
			t.Fatalf("Permissions = %v, %v", set, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("source called %d times, want 1", calls.Load())
	}

	time.Sleep(60 * time.Millisecond)
	if _, err := cache.Permissions(context.Background(), user, "Bearer x"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("source called %d times after expiry, want 2", calls.Load())
	}

	cache.Forget(user)
	if _, err := cache.Permissions(context.Background(), user, "Bearer x"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("source called %d times after Forget, want 3", calls.Load())
	}
}

func TestCacheDoesNotStoreFailures(t *testing.T) {
	var calls atomic.Int32
	source := authz.SourceFunc(func(context.Context, uuid.UUID, string) ([]string, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("auth is down")
		}
		return []string{"rbac:read"}, nil
	})
	cache := authz.NewCache(source, time.Minute)
	user := uuid.New()
	if _, err := cache.Permissions(context.Background(), user, ""); err == nil {
		t.Fatal("first lookup should fail")
	}
	set, err := cache.Permissions(context.Background(), user, "")
	if err != nil || !set.Has(authz.RolesRead) {
		t.Fatalf("second lookup = %v, %v", set, err)
	}
}

func TestReason(t *testing.T) {
	tests := []struct {
		raw  string
		want string
		ok   bool
	}{
		{raw: "  spam  ", want: "spam", ok: true},
		{raw: "no", ok: false},
		{raw: "   ", ok: false},
		{raw: strings.Repeat("я", 500), want: strings.Repeat("я", 500), ok: true},
		{raw: strings.Repeat("a", 501), ok: false},
	}
	for _, tt := range tests {
		got, err := authz.Reason(tt.raw)
		if tt.ok != (err == nil) || got != tt.want {
			t.Errorf("Reason(%q) = %q, %v", tt.raw, got, err)
		}
		if err != nil && connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("Reason(%q) code = %v", tt.raw, connect.CodeOf(err))
		}
	}
}

func TestActionEvent(t *testing.T) {
	actor := uuid.New()
	ctx := authz.WithActor(context.Background(), authz.Actor{UserID: actor, ClientIP: "10.0.0.1", UserAgent: "test"})
	action := authz.NewAction(ctx, "game", "session_cancelled", "game_session", "s-1", "cheating")
	action.Details = map[string]string{"game": "chess"}
	id := uuid.New()
	event := action.Event(id)

	if event.Topic != authz.Topic || event.AggregateID != "s-1" || event.ID != id {
		t.Fatalf("event = %+v", event)
	}
	payload := event.Payload.(*eventsv1.AdminAction)
	if payload.GetActorId() != actor.String() || payload.GetOutcome() != eventsv1.AdminOutcome_ADMIN_OUTCOME_SUCCEEDED ||
		payload.GetReason() != "cheating" || payload.GetClientIp() != "10.0.0.1" || payload.GetDetails()["game"] != "chess" {
		t.Fatalf("payload = %v", payload)
	}

	empty := authz.NewAction(context.Background(), "auth", "x", "", "", "").Event(uuid.New())
	if empty.AggregateID != "auth" {
		t.Fatalf("aggregate without target = %q", empty.AggregateID)
	}
}

type staticVerifier map[string]uuid.UUID

func (v staticVerifier) Verify(raw string) (authn.Claims, error) {
	user, ok := v[raw]
	if !ok {
		return authn.Claims{}, authn.ErrInvalidToken
	}
	return authn.Claims{UserID: user}, nil
}

type permissionsHandler struct {
	authv1connect.UnimplementedAuthAdminServiceHandler
	seen chan authz.Actor
}

func (h *permissionsHandler) GetMyPermissions(ctx context.Context, _ *connect.Request[authv1.GetMyPermissionsRequest]) (*connect.Response[authv1.GetMyPermissionsResponse], error) {
	actor, _ := authz.ActorFrom(ctx)
	h.seen <- actor
	return connect.NewResponse(&authv1.GetMyPermissionsResponse{}), nil
}

func (h *permissionsHandler) ListUsers(context.Context, *connect.Request[authv1.ListUsersRequest]) (*connect.Response[authv1.ListUsersResponse], error) {
	return connect.NewResponse(&authv1.ListUsersResponse{}), nil
}

func TestInterceptor(t *testing.T) {
	admin, viewer := uuid.New(), uuid.New()
	verifier := staticVerifier{"admin": admin, "viewer": viewer}
	granted := map[uuid.UUID][]string{admin: {"admin.users:read", "admin.users:mutate"}, viewer: {"user:read"}}
	cache := authz.NewCache(authz.SourceFunc(func(_ context.Context, user uuid.UUID, _ string) ([]string, error) {
		return granted[user], nil
	}), time.Minute)

	var recorded []authz.Action
	handler := &permissionsHandler{seen: make(chan authz.Actor, 1)}
	path, h := authv1connect.NewAuthAdminServiceHandler(handler, connect.WithInterceptors(authz.NewInterceptor(authz.Config{
		Service:  "auth",
		Verifier: verifier,
		Cache:    cache,
		Rules:    authz.Rules{authv1connect.AuthAdminServiceGetMyPermissionsProcedure: authz.UsersRead},
		Record: func(_ context.Context, action authz.Action) error {
			recorded = append(recorded, action)
			return nil
		},
	})))
	mux := http.NewServeMux()
	mux.Handle(path, h)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := authv1connect.NewAuthAdminServiceClient(server.Client(), server.URL)

	call := func(token string) error {
		req := connect.NewRequest(&authv1.GetMyPermissionsRequest{})
		if token != "" {
			req.Header().Set(authn.Header, "Bearer "+token)
		}
		req.Header().Set(authn.ClientIPHeader, "192.0.2.7")
		_, err := client.GetMyPermissions(context.Background(), req)
		return err
	}

	if err := call(""); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("no token: %v", err)
	}
	if err := call("forged"); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("bad token: %v", err)
	}
	if err := call("viewer"); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("viewer: %v", err)
	}
	if len(recorded) != 1 {
		t.Fatalf("recorded %d denials, want 1", len(recorded))
	}
	denial := recorded[0].Event(uuid.New()).Payload.(*eventsv1.AdminAction)
	if denial.GetOutcome() != eventsv1.AdminOutcome_ADMIN_OUTCOME_DENIED || denial.GetActorId() != viewer.String() ||
		denial.GetTargetId() != authv1connect.AuthAdminServiceGetMyPermissionsProcedure || denial.GetClientIp() != "192.0.2.7" {
		t.Fatalf("denial = %v", denial)
	}

	if err := call("admin"); err != nil {
		t.Fatalf("admin: %v", err)
	}
	actor := <-handler.seen
	if actor.UserID != admin || !actor.Permissions.Has(authz.UsersMutate) {
		t.Fatalf("actor = %+v", actor)
	}

	listReq := connect.NewRequest(&authv1.ListUsersRequest{})
	listReq.Header().Set(authn.Header, "Bearer admin")
	if _, err := client.ListUsers(context.Background(), listReq); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("unmapped procedure: %v", err)
	}
}
