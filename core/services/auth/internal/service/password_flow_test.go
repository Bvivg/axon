package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

func TestRegisterCreatesAnAccount(t *testing.T) {
	h := newHarness(t)

	res, err := h.svc.Register(context.Background(), service.RegisterInput{
		Email:       "Bob@Example.COM",
		Password:    validPassword,
		DisplayName: "  Bob  ",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if res.User.Email != "bob@example.com" {
		t.Errorf("Email = %q, want the normalized form", res.User.Email)
	}
	if res.User.DisplayName != "Bob" {
		t.Errorf("DisplayName = %q, want the trimmed form", res.User.DisplayName)
	}
	if res.User.ID == uuid.Nil {
		t.Error("the new account has no id")
	}
	if res.User.EmailVerified {
		t.Error("a freshly registered address is marked verified")
	}
	if res.Tokens.AccessToken == "" || res.Tokens.RefreshToken == "" {
		t.Error("registration did not sign the user in")
	}
}

// Unlike login, registration may say the address is taken: it is already
// visibly in use to whoever owns it, and hiding that would leave the caller
// with an unexplainable failure.
func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	h := newHarness(t)

	h.register(t, "bob@example.com", validPassword)

	_, err := h.svc.Register(context.Background(), service.RegisterInput{
		// Different casing, same account.
		Email:    "BOB@example.com",
		Password: validPassword,
	})
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("Register error = %v, want ErrEmailTaken", err)
	}
}

func TestRegisterValidatesInput(t *testing.T) {
	h := newHarness(t)

	tests := map[string]service.RegisterInput{
		"no email":           {Password: validPassword},
		"malformed email":    {Email: "not-an-address", Password: validPassword},
		"no password":        {Email: "bob@example.com"},
		"password too short": {Email: "bob@example.com", Password: "short"},
		"password too long": {
			Email: "bob@example.com", Password: strings.Repeat("a", domain.MaxPasswordLength+1),
		},
		"display name too long": {
			Email:       "bob@example.com",
			Password:    validPassword,
			DisplayName: strings.Repeat("б", domain.MaxDisplayNameLength+1),
		},
	}

	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := h.svc.Register(context.Background(), in)
			if err == nil {
				t.Fatal("invalid input was accepted")
			}
			if _, ok := domain.AsValidationError(err); !ok {
				t.Fatalf("error = %v, want a ValidationError naming the field", err)
			}
		})
	}
}

func TestLogin(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)

	res, err := h.svc.Login(context.Background(), service.LoginInput{
		// Casing must not matter.
		Email:    "BOB@Example.com",
		Password: validPassword,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if res.User.ID != registered.User.ID {
		t.Errorf("signed in as %v, want %v", res.User.ID, registered.User.ID)
	}
	if res.Tokens.RefreshToken == registered.Tokens.RefreshToken {
		t.Error("a second sign-in reused the first sign-in's refresh token")
	}
}

// A wrong password and an unknown address must be indistinguishable, or login
// becomes a way to find out which addresses have accounts.
func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	h := newHarness(t)

	h.register(t, "bob@example.com", validPassword)

	tests := map[string]service.LoginInput{
		"wrong password": {Email: "bob@example.com", Password: "not the password"},
		"unknown email":  {Email: "nobody@example.com", Password: validPassword},
		"empty password": {Email: "bob@example.com", Password: ""},
	}

	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := h.svc.Login(context.Background(), in)
			if !errors.Is(err, domain.ErrInvalidCredentials) {
				t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

// Identical error messages are pointless if the response time still says whether
// the address exists. The unknown-email path has to spend the same work a real
// verification does.
//
// The bound is deliberately loose — this is a shared CI machine, not a lab — but
// it still catches the regression cleanly: without the placeholder verification
// the unknown-email path is a map lookup, orders of magnitude faster than argon2.
func TestUnknownEmailCostsTheSameAsAWrongPassword(t *testing.T) {
	h := newHarness(t)

	h.register(t, "bob@example.com", validPassword)

	measure := func(in service.LoginInput) time.Duration {
		const runs = 5

		var total time.Duration
		for range runs {
			start := time.Now()
			if _, err := h.svc.Login(context.Background(), in); err == nil {
				t.Fatal("the login under measurement unexpectedly succeeded")
			}
			total += time.Since(start)
		}
		return total / runs
	}

	wrongPassword := measure(service.LoginInput{Email: "bob@example.com", Password: "wrong"})
	unknownEmail := measure(service.LoginInput{Email: "nobody@example.com", Password: "wrong"})

	if unknownEmail < wrongPassword/4 {
		t.Fatalf("unknown email took %v against %v for a wrong password — the placeholder verification is missing, "+
			"which makes login a user-enumeration oracle", unknownEmail, wrongPassword)
	}
}

// An account that only ever signed in through a provider has no password at all.
// It must answer like any other failed sign-in, so the response does not reveal
// how the account was created.
func TestLoginAgainstAnOauthOnlyAccount(t *testing.T) {
	h := newHarness(t)

	user := domain.User{ID: uuid.New(), Email: "bob@example.com"}
	if _, err := h.store.CreateUserWithOauthAccount(context.Background(), user, domain.OauthAccount{
		Provider: domain.ProviderGoogle, ProviderUserID: "google-123", Email: user.Email,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	start := time.Now()
	_, err := h.svc.Login(context.Background(), service.LoginInput{
		Email: "bob@example.com", Password: validPassword,
	})
	elapsed := time.Since(start)

	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
	}
	// Same reasoning as above: a fast "no password here" would leak that the
	// account exists and is provider-only.
	if elapsed < time.Millisecond {
		t.Fatalf("the no-password path returned in %v without doing any work", elapsed)
	}
}

// A corrupted hash is an operational failure. Reporting it as a wrong password
// would hide a real problem behind a routine-looking response.
func TestUnreadableStoredHashIsNotAWrongPassword(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)

	if err := h.store.SetCredential(context.Background(), registered.User.ID, "corrupted"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}

	_, err := h.svc.Login(context.Background(), service.LoginInput{
		Email: "bob@example.com", Password: validPassword,
	})
	if err == nil {
		t.Fatal("a corrupted hash allowed a sign-in")
	}
	if errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatal("a corrupted stored hash was reported as a wrong password")
	}
}

func TestMe(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "bob@example.com", validPassword)

	got, err := h.svc.Me(context.Background(), registered.User.ID)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if got.ID != registered.User.ID || got.Email != "bob@example.com" {
		t.Fatalf("Me returned %+v, want the registered user", got)
	}

	if _, err := h.svc.Me(context.Background(), uuid.New()); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("Me for an unknown id = %v, want ErrUserNotFound", err)
	}
}

func TestNewValidatesItsDependencies(t *testing.T) {
	h := newHarness(t)

	// A zero refresh lifetime would issue instantly-dead tokens.
	if _, err := service.New(h.store, nil, nil, nil, service.Config{}); err == nil {
		t.Fatal("a service with no dependencies was constructed")
	}
}
