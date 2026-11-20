package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

var auditDevice = domain.Device{UserAgent: "Firefox/140", IP: "203.0.113.7"}

type countingCounter struct{ n atomic.Int64 }

func (c *countingCounter) Inc() { c.n.Add(1) }

func withAuditFailures(c *countingCounter) harnessOption {
	return func(cfg *service.Config) { cfg.AuditFailures = c }
}

func onDevice(t *testing.T) context.Context {
	return domain.ContextWithDevice(t.Context(), auditDevice)
}

func TestSecurityEventsPerFlow(t *testing.T) {
	tests := []struct {
		name string
		opts func(*testing.T) []harnessOption
		run  func(*testing.T, *harness) []domain.SecurityEvent
	}{
		{
			name: "password sign-in",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				user := h.register(t, "ada@axon.test", validPassword).User
				res, err := h.svc.Login(onDevice(t), service.LoginInput{Email: "Ada@Axon.Test", Password: validPassword})
				if err != nil {
					t.Fatalf("Login: %v", err)
				}
				family := h.storedToken(t, res.Tokens.RefreshToken).FamilyID
				return []domain.SecurityEvent{{Action: domain.SecurityLoginSucceeded, UserID: user.ID, ActorID: user.ID, Subject: family.String()}}
			},
		},
		{
			name: "unknown email",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				_, err := h.svc.Login(onDevice(t), service.LoginInput{Email: " Nobody@Axon.Test", Password: validPassword})
				if !errors.Is(err, domain.ErrInvalidCredentials) {
					t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
				}
				return []domain.SecurityEvent{{Action: domain.SecurityLoginFailed, EmailSHA256: domain.EmailSHA256("nobody@axon.test")}}
			},
		},
		{
			name: "wrong password",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				user := h.register(t, "ada@axon.test", validPassword).User
				_, err := h.svc.Login(onDevice(t), service.LoginInput{Email: "ada@axon.test", Password: "not the password at all"})
				if !errors.Is(err, domain.ErrInvalidCredentials) {
					t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
				}
				return []domain.SecurityEvent{{Action: domain.SecurityLoginFailed, UserID: user.ID, EmailSHA256: domain.EmailSHA256("ada@axon.test")}}
			},
		},
		{
			name: "account without a password",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				user, err := h.store.CreateUserWithOauthAccount(t.Context(), domain.User{ID: uuid.New(), Email: "ada@axon.test", Username: "ada"},
					domain.OauthAccount{Provider: domain.ProviderGoogle, ProviderUserID: "google-ada", Email: "ada@axon.test"})
				if err != nil {
					t.Fatal(err)
				}
				_, err = h.svc.Login(onDevice(t), service.LoginInput{Email: "ada@axon.test", Password: validPassword})
				if !errors.Is(err, domain.ErrInvalidCredentials) {
					t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
				}
				return []domain.SecurityEvent{{Action: domain.SecurityLoginFailed, UserID: user.ID, EmailSHA256: domain.EmailSHA256("ada@axon.test")}}
			},
		},
		{
			name: "oauth sign-in",
			opts: func(t *testing.T) []harnessOption { return []harnessOption{withOAuth(t)} },
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				started, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, "")
				if err != nil {
					t.Fatalf("StartOAuth: %v", err)
				}
				code := h.follow(t, started.AuthorizationURL, identity("fake-ada", "ada@axon.test"))
				res, err := h.svc.CompleteOAuth(onDevice(t), service.CompleteOAuthInput{Provider: domain.ProviderFake, Code: code, State: started.State})
				if err != nil {
					t.Fatalf("CompleteOAuth: %v", err)
				}
				return []domain.SecurityEvent{{Action: domain.SecurityLoginSucceeded, UserID: res.User.ID, ActorID: res.User.ID, Subject: "fake"}}
			},
		},
		{
			name: "logout",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				registered := h.register(t, "ada@axon.test", validPassword)
				family := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID
				if err := h.svc.Logout(onDevice(t), registered.Tokens.RefreshToken); err != nil {
					t.Fatalf("Logout: %v", err)
				}
				return []domain.SecurityEvent{{Action: domain.SecurityLoggedOut, UserID: registered.User.ID, ActorID: registered.User.ID, Subject: family.String()}}
			},
		},
		{
			name: "logout with an unknown token",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				if err := h.svc.Logout(onDevice(t), "not-a-real-token"); err != nil {
					t.Fatalf("Logout: %v", err)
				}
				return nil
			},
		},
		{
			name: "logout everywhere",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				user := h.register(t, "ada@axon.test", validPassword).User
				if err := h.svc.LogoutEverywhere(onDevice(t), user.ID); err != nil {
					t.Fatalf("LogoutEverywhere: %v", err)
				}
				return []domain.SecurityEvent{{Action: domain.SecurityLoggedOutEverywhere, UserID: user.ID, ActorID: user.ID}}
			},
		},
		{
			name: "session revoked",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				registered := h.register(t, "ada@axon.test", validPassword)
				family := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID
				if err := h.svc.RevokeSession(onDevice(t), registered.User.ID, family); err != nil {
					t.Fatalf("RevokeSession: %v", err)
				}
				return []domain.SecurityEvent{{Action: domain.SecuritySessionRevoked, UserID: registered.User.ID, ActorID: registered.User.ID, Subject: family.String()}}
			},
		},
		{
			name: "revoking a session of another account",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				victim := h.register(t, "ada@axon.test", validPassword)
				attacker := h.register(t, "eve@axon.test", validPassword).User
				family := h.storedToken(t, victim.Tokens.RefreshToken).FamilyID
				if err := h.svc.RevokeSession(onDevice(t), attacker.ID, family); !errors.Is(err, domain.ErrSessionNotFound) {
					t.Fatalf("RevokeSession error = %v, want ErrSessionNotFound", err)
				}
				return nil
			},
		},
		{
			name: "session revocation the store rejects",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				registered := h.register(t, "ada@axon.test", validPassword)
				family := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID
				h.store.failOn["RevokeFamilyForUser"] = errors.New("database unavailable")
				if err := h.svc.RevokeSession(onDevice(t), registered.User.ID, family); err == nil {
					t.Fatal("RevokeSession succeeded although the store failed")
				}
				return nil
			},
		},
		{
			name: "refresh rotation",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				registered := h.register(t, "ada@axon.test", validPassword)
				if _, err := h.svc.Refresh(onDevice(t), registered.Tokens.RefreshToken, auditDevice); err != nil {
					t.Fatalf("Refresh: %v", err)
				}
				return nil
			},
		},
		{
			name: "refresh token replayed",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				registered := h.register(t, "ada@axon.test", validPassword)
				stolen := registered.Tokens.RefreshToken
				family := h.storedToken(t, stolen).FamilyID
				if _, err := h.svc.Refresh(t.Context(), stolen, domain.Device{}); err != nil {
					t.Fatalf("Refresh: %v", err)
				}
				if _, err := h.svc.Refresh(onDevice(t), stolen, auditDevice); !errors.Is(err, domain.ErrRefreshTokenReused) {
					t.Fatalf("replay error = %v, want ErrRefreshTokenReused", err)
				}
				return []domain.SecurityEvent{{Action: domain.SecurityRefreshTokenReused, UserID: registered.User.ID, Subject: family.String()}}
			},
		},
		{
			name: "refresh exchanges racing for one token",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				registered := h.register(t, "ada@axon.test", validPassword)
				stolen := registered.Tokens.RefreshToken
				family := h.storedToken(t, stolen).FamilyID
				var raced bool
				h.store.beforeRotate = func() {
					if raced {
						return
					}
					raced = true
					if _, err := h.svc.Refresh(t.Context(), stolen, domain.Device{}); err != nil {
						t.Errorf("the competing refresh failed: %v", err)
					}
				}
				if _, err := h.svc.Refresh(onDevice(t), stolen, auditDevice); !errors.Is(err, domain.ErrRefreshTokenReused) {
					t.Fatalf("losing exchange error = %v, want ErrRefreshTokenReused", err)
				}
				return []domain.SecurityEvent{{Action: domain.SecurityRefreshTokenReused, UserID: registered.User.ID, Subject: family.String()}}
			},
		},
		{
			name: "role assigned",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				admin, target := adminAndTarget(t, h)
				if err := h.svc.AssignRole(onDevice(t), admin, target, domain.RoleAdmin); err != nil {
					t.Fatalf("AssignRole: %v", err)
				}
				return []domain.SecurityEvent{{Action: domain.SecurityRoleAssigned, UserID: target, ActorID: admin, Subject: domain.RoleAdmin.String()}}
			},
		},
		{
			name: "role revoked",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				admin, target := adminAndTarget(t, h)
				if err := h.svc.RevokeRole(onDevice(t), admin, target, domain.RoleUser); err != nil {
					t.Fatalf("RevokeRole: %v", err)
				}
				return []domain.SecurityEvent{{Action: domain.SecurityRoleRevoked, UserID: target, ActorID: admin, Subject: domain.RoleUser.String()}}
			},
		},
		{
			name: "role change without permission",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				_, target := adminAndTarget(t, h)
				if err := h.svc.AssignRole(onDevice(t), target, target, domain.RoleAdmin); !errors.Is(err, domain.ErrPermissionDenied) {
					t.Fatalf("AssignRole error = %v, want ErrPermissionDenied", err)
				}
				return nil
			},
		},
		{
			name: "revoking a role the user does not hold",
			run: func(t *testing.T, h *harness) []domain.SecurityEvent {
				admin, target := adminAndTarget(t, h)
				if err := h.svc.RevokeRole(onDevice(t), admin, target, domain.RoleAdmin); !errors.Is(err, domain.ErrRoleNotFound) {
					t.Fatalf("RevokeRole error = %v, want ErrRoleNotFound", err)
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []harnessOption
			if tt.opts != nil {
				opts = tt.opts(t)
			}
			h := newHarness(t, opts...)

			want := tt.run(t, h)
			got := h.store.recordedSecurityEvents()
			if len(got) != len(want) {
				t.Fatalf("recorded %d security events %v, want %d %v", len(got), actionsOf(got), len(want), actionsOf(want))
			}
			for i := range want {
				want[i].Device = auditDevice
				if !got[i].OccurredAt.Equal(h.now()) {
					t.Errorf("event %d occurred_at = %v, want %v", i, got[i].OccurredAt, h.now())
				}
				got[i].OccurredAt = want[i].OccurredAt
				if got[i] != want[i] {
					t.Errorf("event %d = %+v\nwant %+v", i, got[i], want[i])
				}
				if strings.Contains(fmt.Sprintf("%+v", got[i]), "@axon.test") {
					t.Errorf("event %d carries a raw email address: %+v", i, got[i])
				}
			}
		})
	}
}

func TestFailedAuditWriteDoesNotFailTheRequest(t *testing.T) {
	tests := []struct {
		name    string
		run     func(*testing.T, *harness) error
		wantErr error
	}{
		{
			name: "password sign-in",
			run: func(t *testing.T, h *harness) error {
				h.register(t, "ada@axon.test", validPassword)
				_, err := h.svc.Login(onDevice(t), service.LoginInput{Email: "ada@axon.test", Password: validPassword})
				return err
			},
		},
		{
			name: "unknown email",
			run: func(t *testing.T, h *harness) error {
				_, err := h.svc.Login(onDevice(t), service.LoginInput{Email: "nobody@axon.test", Password: validPassword})
				return err
			},
			wantErr: domain.ErrInvalidCredentials,
		},
		{
			name: "wrong password",
			run: func(t *testing.T, h *harness) error {
				h.register(t, "ada@axon.test", validPassword)
				_, err := h.svc.Login(onDevice(t), service.LoginInput{Email: "ada@axon.test", Password: "not the password at all"})
				return err
			},
			wantErr: domain.ErrInvalidCredentials,
		},
		{
			name: "logout",
			run: func(t *testing.T, h *harness) error {
				registered := h.register(t, "ada@axon.test", validPassword)
				return h.svc.Logout(onDevice(t), registered.Tokens.RefreshToken)
			},
		},
		{
			name: "session revoked",
			run: func(t *testing.T, h *harness) error {
				registered := h.register(t, "ada@axon.test", validPassword)
				return h.svc.RevokeSession(onDevice(t), registered.User.ID, h.storedToken(t, registered.Tokens.RefreshToken).FamilyID)
			},
		},
		{
			name: "role assigned",
			run: func(t *testing.T, h *harness) error {
				admin, target := adminAndTarget(t, h)
				return h.svc.AssignRole(onDevice(t), admin, target, domain.RoleAdmin)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failures := &countingCounter{}
			h := newHarness(t, withAuditFailures(failures))
			h.store.failOn["RecordSecurityEvent"] = errors.New("outbox unavailable")

			if err := tt.run(t, h); !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if n := failures.n.Load(); n != 1 {
				t.Fatalf("audit failures counted = %d, want 1", n)
			}
		})
	}
}

func TestSecurityEventOutlivesACancelledRequest(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@axon.test", validPassword)

	ctx, cancel := context.WithCancel(onDevice(t))
	cancel()

	if err := h.svc.Logout(ctx, registered.Tokens.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if got := actionsOf(h.store.recordedSecurityEvents()); len(got) != 1 || got[0] != domain.SecurityLoggedOut {
		t.Fatalf("recorded %v, want [%s]", got, domain.SecurityLoggedOut)
	}
}

func adminAndTarget(t *testing.T, h *harness) (uuid.UUID, uuid.UUID) {
	t.Helper()

	admin := h.register(t, "admin@axon.test", validPassword).User.ID
	if err := h.store.AssignRole(t.Context(), admin, domain.RoleAdmin); err != nil {
		t.Fatalf("grant admin: %v", err)
	}
	return admin, h.register(t, "target@axon.test", validPassword).User.ID
}

func actionsOf(events []domain.SecurityEvent) []domain.SecurityAction {
	out := make([]domain.SecurityAction, len(events))
	for i, e := range events {
		out[i] = e.Action
	}
	return out
}
