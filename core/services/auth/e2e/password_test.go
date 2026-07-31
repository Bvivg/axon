//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
)

// The flow rules/testing.md names first: register, sign in, reach a protected
// method with what that produced.
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

	// A separate sign-in must produce a token that works just as well: the two
	// paths issue tokens through different code, and only one of them is
	// exercised by registration.
	tokens, err := c.login(acct.email, testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := c.getMe(tokens.GetAccessToken()); err != nil {
		t.Fatalf("GetMe with the login token: %v", err)
	}
}

// Email is normalised before it is stored, so a differently-cased address is
// the same account — including for the uniqueness constraint.
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

// Sign-in must not tell an attacker whether an address has an account. The code
// and the message have to be identical, not merely similar: a difference in
// either turns login into a membership oracle.
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

// The reason refresh tokens are rotated at all. A token presented twice means
// either a replay or a leak, and the only safe reading is the second one — so
// the whole chain descended from it dies, including the successor held by
// whoever is legitimate. They sign in again; an attacker holding a stolen token
// gets nothing.
//
// There is no way to observe this short of the full stack: it spans the service
// layer, two tables and a transaction.
func TestReplayingASpentRefreshTokenRevokesTheWholeChain(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	spent := acct.tokens.GetRefreshToken()

	successor, err := c.refresh(spent)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	// The replay itself is refused.
	_, replay := c.refresh(spent)
	requireCode(t, replay, connect.CodeUnauthenticated)

	// And the successor, which was valid a moment ago, is now dead too.
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

// Logout is answered the same way whatever it is given: telling a caller that
// the token they presented was unknown is a small oracle, and there is nothing
// useful for a client to do with the distinction.
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

// The gateway refuses these itself, before anything reaches auth.
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

// Sign-in runs on the tight budget. The exact threshold is a configuration
// detail, so the assertion is the behaviour: a caller who keeps trying is
// eventually refused, and refused with the code that says so rather than with a
// credential error.
func TestSignInIsRateLimited(t *testing.T) {
	c := newCaller(t)

	const attempts = 50

	allowed := 0
	for range attempts {
		_, err := c.login(email(), testPassword)
		if connect.CodeOf(err) == connect.CodeResourceExhausted {
			// A caller throttled on their very first request proves nothing:
			// the budget has to be something they can spend before it runs out.
			if allowed == 0 {
				t.Fatal("the first request from a fresh caller was throttled")
			}
			return
		}
		// Anything other than a credential failure means the test stopped
		// measuring what it set out to measure.
		requireCode(t, err, connect.CodeUnauthenticated)
		allowed++
	}

	t.Fatalf("%d failed sign-ins from one address were never throttled", attempts)
}

// A throttled caller must not take anyone else down with them.
func TestThrottlingIsPerCaller(t *testing.T) {
	noisy := newCaller(t)
	quiet := newCaller(t)

	// Establish the quiet caller before the noisy one starts, so the account
	// exists without competing for a budget later.
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
