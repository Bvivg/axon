//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
)

func TestRegisterThenLoginThenGetMe(t *testing.T) {
	c := newCaller(t)

	acct := c.register(t)

	user, err := c.getMe(acct.tokens.GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe with the registration token: %v", err)
	}
	if user.GetId() != acct.userID {
		t.Errorf("GetMe returned user %s, want %s", user.GetId(), acct.userID)
	}
	if user.GetEmail() != acct.email {
		t.Errorf("email = %q, want %q", user.GetEmail(), acct.email)
	}

	tokens, err := c.login(acct.email, testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := c.getMe(tokens.GetAccessToken()); err != nil {
		t.Fatalf("GetMe with the login token: %v", err)
	}
}

func TestEmailIsCaseInsensitive(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	upper := strings.ToUpper(acct.email)
	if _, err := c.login(upper, testPassword); err != nil {
		t.Fatalf("login with %q was refused: %v", upper, err)
	}
}

func TestDuplicateRegistrationIsRejected(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	_, err := c.registerAs(acct.email)
	requireCode(t, err, connect.CodeAlreadyExists)
}

func TestFailedLoginDoesNotRevealWhetherTheAccountExists(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	_, wrongPassword := c.login(acct.email, "not-the-password")
	requireCode(t, wrongPassword, connect.CodeUnauthenticated)

	_, unknownAccount := c.login(email(), testPassword)
	requireCode(t, unknownAccount, connect.CodeUnauthenticated)

	if a, b := connect.CodeOf(wrongPassword), connect.CodeOf(unknownAccount); a != b {
		t.Errorf("codes differ: wrong password %v, unknown account %v", a, b)
	}
	if a, b := wrongPassword.Error(), unknownAccount.Error(); a != b {
		t.Errorf("messages differ:\n  wrong password:  %q\n  unknown account: %q", a, b)
	}
}

func TestRefreshRotatesTheToken(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	next, err := c.refresh(acct.tokens.GetRefreshToken())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if next.GetRefreshToken() == acct.tokens.GetRefreshToken() {
		t.Fatal("refresh returned the same token: it is not rotating")
	}
	if _, err := c.getMe(next.GetAccessToken()); err != nil {
		t.Fatalf("the access token from a refresh does not work: %v", err)
	}
}

func TestReplayingASpentRefreshTokenRevokesTheWholeChain(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	spent := acct.tokens.GetRefreshToken()

	successor, err := c.refresh(spent)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	_, replay := c.refresh(spent)
	requireCode(t, replay, connect.CodeUnauthenticated)

	_, afterRevocation := c.refresh(successor.GetRefreshToken())
	requireCode(t, afterRevocation, connect.CodeUnauthenticated)
}

func TestLogoutInvalidatesTheRefreshToken(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	if err := c.logout(acct.tokens.GetRefreshToken()); err != nil {
		t.Fatalf("logout: %v", err)
	}

	_, err := c.refresh(acct.tokens.GetRefreshToken())
	requireCode(t, err, connect.CodeUnauthenticated)
}

func TestLogoutIsIdempotent(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	if err := c.logout(acct.tokens.GetRefreshToken()); err != nil {
		t.Fatalf("first logout: %v", err)
	}
	if err := c.logout(acct.tokens.GetRefreshToken()); err != nil {
		t.Fatalf("second logout was refused: %v", err)
	}
}

func TestProtectedProcedureRejectsBadTokens(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	valid := acct.tokens.GetAccessToken()

	tests := map[string]string{
		"no token":     "",
		"not a token":  "definitely-not-a-jwt",
		"tampered":     valid[:len(valid)-4] + "AAAA",
		"empty bearer": " ",
	}

	for name, token := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := c.getMe(token)
			requireCode(t, err, connect.CodeUnauthenticated)
		})
	}
}

func TestSignInIsRateLimited(t *testing.T) {
	c := newCaller(t)

	const attempts = 50

	allowed := 0
	for range attempts {
		_, err := c.login(email(), testPassword)
		if connect.CodeOf(err) == connect.CodeResourceExhausted {

			if allowed == 0 {
				t.Fatal("the first request from a fresh caller was throttled")
			}
			return
		}

		requireCode(t, err, connect.CodeUnauthenticated)
		allowed++
	}

	t.Fatalf("%d failed sign-ins from one address were never throttled", attempts)
}

func TestThrottlingIsPerCaller(t *testing.T) {
	noisy := newCaller(t)
	quiet := newCaller(t)

	acct := quiet.register(t)

	throttled := false
	for range 50 {
		if _, err := noisy.login(email(), testPassword); connect.CodeOf(err) == connect.CodeResourceExhausted {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Fatal("the noisy caller was never throttled, so this proves nothing")
	}

	if _, err := quiet.login(acct.email, testPassword); err != nil {
		t.Fatalf("a second caller was throttled by someone else's traffic: %v", err)
	}
}
