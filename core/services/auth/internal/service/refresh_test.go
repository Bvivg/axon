package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

func TestRefreshRotatesTheToken(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)

	refreshed, err := h.svc.Refresh(context.Background(), registered.Tokens.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if refreshed.Tokens.RefreshToken == registered.Tokens.RefreshToken {
		t.Fatal("Refresh returned the same refresh token; it was not rotated")
	}
	if refreshed.Tokens.AccessToken == "" {
		t.Fatal("Refresh returned no access token")
	}
	if refreshed.User.ID != registered.User.ID {
		t.Fatalf("Refresh returned user %v, want %v", refreshed.User.ID, registered.User.ID)
	}

	// The presented token must be spent, and its successor live.
	spent := h.storedToken(t, registered.Tokens.RefreshToken)
	if !spent.Used() {
		t.Error("the presented token was not marked used")
	}

	successor := h.storedToken(t, refreshed.Tokens.RefreshToken)
	if !successor.Usable(h.now()) {
		t.Error("the successor token is not usable")
	}
	// Same chain: the successor belongs to the original sign-in.
	if successor.FamilyID != spent.FamilyID {
		t.Error("the successor is in a different family than the token it replaced")
	}
}

func TestRefreshChainWorksRepeatedly(t *testing.T) {
	h := newHarness(t)

	current := h.register(t, "bob@example.com", validPassword).Tokens.RefreshToken

	for i := range 5 {
		next, err := h.svc.Refresh(context.Background(), current)
		if err != nil {
			t.Fatalf("refresh %d: %v", i, err)
		}
		current = next.Tokens.RefreshToken
	}
}

// The central guarantee of the whole design: presenting a token that was already
// exchanged is treated as theft, and the entire chain dies — including the
// successor the legitimate client is holding.
func TestReusedTokenRevokesTheWholeFamily(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)
	stolen := registered.Tokens.RefreshToken

	// The real client refreshes once, so `stolen` is now spent and the client
	// holds its successor.
	legitimate, err := h.svc.Refresh(context.Background(), stolen)
	if err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	// The thief replays the token they captured.
	_, err = h.svc.Refresh(context.Background(), stolen)
	if !errors.Is(err, domain.ErrRefreshTokenReused) {
		t.Fatalf("replay error = %v, want ErrRefreshTokenReused", err)
	}

	// The successor the real client still holds must now be dead too. Signing the
	// real user out is the intended cost: there is no way to tell the two holders
	// apart, and leaving the chain alive would leave the attacker with a session.
	if _, err := h.svc.Refresh(context.Background(), legitimate.Tokens.RefreshToken); err == nil {
		t.Fatal("the legitimate successor still worked after reuse was detected")
	}

	if live := h.store.liveTokensForUser(registered.User.ID, h.now()); live != 0 {
		t.Fatalf("%d tokens are still live after the family was revoked", live)
	}
}

// A second sign-in is a separate chain, so revoking one device must not sign the
// other out.
func TestReuseDoesNotAffectOtherSignIns(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)

	// Second device.
	other, err := h.svc.Login(context.Background(), service.LoginInput{
		Email: "bob@example.com", Password: validPassword,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// Compromise the first chain.
	stolen := registered.Tokens.RefreshToken
	if _, err := h.svc.Refresh(context.Background(), stolen); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	if _, err := h.svc.Refresh(context.Background(), stolen); !errors.Is(err, domain.ErrRefreshTokenReused) {
		t.Fatalf("replay error = %v, want ErrRefreshTokenReused", err)
	}

	// The other device is untouched.
	if _, err := h.svc.Refresh(context.Background(), other.Tokens.RefreshToken); err != nil {
		t.Fatalf("the second sign-in was revoked along with the first: %v", err)
	}
}

// The other ordering: two exchanges racing with the same token. The state check
// cannot catch this one, because both read the token while it was still unspent.
// Whichever loses the write is treated as reuse.
func TestConcurrentExchangesAreTreatedAsReuse(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)
	stolen := registered.Tokens.RefreshToken

	// Runs inside RotateRefreshToken, after this call has already read the token
	// as unspent — exactly the window a real race opens.
	var once bool
	h.store.beforeRotate = func() {
		if once {
			return
		}
		once = true

		// A competing exchange lands first and wins.
		if _, err := h.svc.Refresh(context.Background(), stolen); err != nil {
			t.Errorf("the competing refresh failed: %v", err)
		}
	}

	_, err := h.svc.Refresh(context.Background(), stolen)
	if !errors.Is(err, domain.ErrRefreshTokenReused) {
		t.Fatalf("the losing exchange returned %v, want ErrRefreshTokenReused", err)
	}

	// And the family is gone, so the winner's successor is worthless too.
	if live := h.store.liveTokensForUser(registered.User.ID, h.now()); live != 0 {
		t.Fatalf("%d tokens are still live after a raced exchange", live)
	}
}

func TestRefreshRejectsUnknownToken(t *testing.T) {
	h := newHarness(t)

	for _, presented := range []string{"", "not-a-real-token"} {
		_, err := h.svc.Refresh(context.Background(), presented)
		if !errors.Is(err, domain.ErrRefreshTokenInvalid) {
			t.Errorf("Refresh(%q) error = %v, want ErrRefreshTokenInvalid", presented, err)
		}
	}
}

func TestRefreshRejectsExpiredToken(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)

	h.advance(refreshTTL + time.Second)

	_, err := h.svc.Refresh(context.Background(), registered.Tokens.RefreshToken)
	if !errors.Is(err, domain.ErrRefreshTokenInvalid) {
		t.Fatalf("Refresh error = %v, want ErrRefreshTokenInvalid", err)
	}
}

// A revoked token is ordinary rejection, not a security event: it is what the
// legitimate client holds after their family was killed, and treating it as
// fresh reuse would revoke an already-dead family on every retry.
func TestRevokedTokenIsInvalidNotReuse(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)

	if err := h.svc.Logout(context.Background(), registered.Tokens.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	_, err := h.svc.Refresh(context.Background(), registered.Tokens.RefreshToken)
	if !errors.Is(err, domain.ErrRefreshTokenInvalid) {
		t.Fatalf("Refresh error = %v, want ErrRefreshTokenInvalid", err)
	}
	if errors.Is(err, domain.ErrRefreshTokenReused) {
		t.Fatal("a revoked token was reported as reuse")
	}
}

func TestLogoutRevokesTheFamily(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)

	// Refresh once so the family has more than one member.
	refreshed, err := h.svc.Refresh(context.Background(), registered.Tokens.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if err := h.svc.Logout(context.Background(), refreshed.Tokens.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if live := h.store.liveTokensForUser(registered.User.ID, h.now()); live != 0 {
		t.Fatalf("%d tokens are still live after logout", live)
	}
}

// Logout must not be a way to find out which tokens exist.
func TestLogoutIsIdempotentAndQuiet(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)

	for _, presented := range []string{
		registered.Tokens.RefreshToken,
		registered.Tokens.RefreshToken, // again
		"never-existed",
		"",
	} {
		if err := h.svc.Logout(context.Background(), presented); err != nil {
			t.Errorf("Logout(%.12q) = %v, want nil", presented, err)
		}
	}
}

func TestLogoutEverywhereRevokesEverySignIn(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)

	second, err := h.svc.Login(context.Background(), service.LoginInput{
		Email: "bob@example.com", Password: validPassword,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if err := h.svc.LogoutEverywhere(context.Background(), registered.User.ID); err != nil {
		t.Fatalf("LogoutEverywhere: %v", err)
	}

	for name, presented := range map[string]string{
		"first":  registered.Tokens.RefreshToken,
		"second": second.Tokens.RefreshToken,
	} {
		if _, err := h.svc.Refresh(context.Background(), presented); err == nil {
			t.Errorf("the %s sign-in still worked after signing out everywhere", name)
		}
	}
}

// If revocation itself fails, the caller is still refused. Surfacing the storage
// error would tell an attacker their replay hit something unexpected.
func TestReuseIsRefusedEvenIfRevocationFails(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)
	stolen := registered.Tokens.RefreshToken

	if _, err := h.svc.Refresh(context.Background(), stolen); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	h.store.failOn["RevokeFamily"] = errors.New("database is on fire")

	_, err := h.svc.Refresh(context.Background(), stolen)
	if !errors.Is(err, domain.ErrRefreshTokenReused) {
		t.Fatalf("error = %v, want ErrRefreshTokenReused", err)
	}
}
