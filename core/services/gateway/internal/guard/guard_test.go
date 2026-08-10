package guard_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/gateway/internal/guard"
	"github.com/bvivg/axon/core/services/gateway/internal/ratelimit"
)

const (
	testIssuer   = "https://auth.axon.test"
	testAudience = "axon"
)

var signingKey *rsa.PrivateKey

func init() {
	var err error
	if signingKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		panic(err)
	}
}

type staticKeys map[string]*rsa.PublicKey

func (s staticKeys) PublicKey(keyID string) (*rsa.PublicKey, bool) {
	k, ok := s[keyID]
	return k, ok
}

// token mints an access token the way auth would.
func token(t *testing.T, mutate func(gojwt.MapClaims)) string {
	t.Helper()

	now := time.Now()
	claims := gojwt.MapClaims{
		"sub":   uuid.NewString(),
		"jti":   uuid.NewString(),
		"iss":   testIssuer,
		"aud":   testAudience,
		"iat":   now.Unix(),
		"exp":   now.Add(15 * time.Minute).Unix(),
		"email": "bob@example.com",
	}
	if mutate != nil {
		mutate(claims)
	}

	jwtToken := gojwt.NewWithClaims(gojwt.SigningMethodRS256, claims)
	jwtToken.Header["kid"] = "dev-1"

	raw, err := jwtToken.SignedString(signingKey)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return raw
}

// stubAuth records what reached it and answers successfully.
type stubAuth struct {
	authv1connect.UnimplementedAuthServiceHandler

	calls int
	// sawClaims records whether the guard put verified claims on the context.
	sawClaims bool
}

func (s *stubAuth) Login(
	ctx context.Context,
	_ *connect.Request[authv1.LoginRequest],
) (*connect.Response[authv1.LoginResponse], error) {
	s.calls++
	_, s.sawClaims = authn.FromContext(ctx)
	return connect.NewResponse(&authv1.LoginResponse{}), nil
}

func (s *stubAuth) GetMe(
	ctx context.Context,
	_ *connect.Request[authv1.GetMeRequest],
) (*connect.Response[authv1.GetMeResponse], error) {
	s.calls++
	_, s.sawClaims = authn.FromContext(ctx)
	return connect.NewResponse(&authv1.GetMeResponse{}), nil
}

// harness wires the guard in front of a stub handler over a real HTTP server,
// so header handling and interceptor ordering are exercised as they run.
type harness struct {
	client authv1connect.AuthServiceClient
	stub   *stubAuth
}

type limits struct {
	standard  int
	sensitive int
}

func newHarness(t *testing.T, l limits) *harness {
	t.Helper()

	verifier, err := authn.NewVerifier(authn.Config{
		Keys:     staticKeys{"dev-1": &signingKey.PublicKey},
		Issuer:   testIssuer,
		Audience: testAudience,
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	if l.standard == 0 {
		l.standard = 1000
	}
	if l.sensitive == 0 {
		l.sensitive = 1000
	}

	g, err := guard.New(guard.Config{
		Verifier:  verifier,
		Standard:  ratelimit.New(ratelimit.Config{PerMinute: l.standard, Burst: l.standard}),
		Sensitive: ratelimit.New(ratelimit.Config{PerMinute: l.sensitive, Burst: l.sensitive}),
		Logger:    logger.Discard(),
	})
	if err != nil {
		t.Fatalf("guard.New: %v", err)
	}

	stub := &stubAuth{}

	mux := http.NewServeMux()
	mux.Handle(authv1connect.NewAuthServiceHandler(stub, connect.WithInterceptors(g)))

	srv := httptest.NewServer(guard.ClientIPMiddleware(0)(mux))
	t.Cleanup(srv.Close)

	return &harness{
		client: authv1connect.NewAuthServiceClient(srv.Client(), srv.URL),
		stub:   stub,
	}
}

// login calls a public procedure, optionally with a token.
func (h *harness) login(accessToken string) error {
	req := connect.NewRequest(&authv1.LoginRequest{})
	if accessToken != "" {
		req.Header().Set(authn.Header, "Bearer "+accessToken)
	}
	_, err := h.client.Login(context.Background(), req)
	return err
}

// getMe calls the protected procedure.
func (h *harness) getMe(accessToken string) error {
	req := connect.NewRequest(&authv1.GetMeRequest{})
	if accessToken != "" {
		req.Header().Set(authn.Header, "Bearer "+accessToken)
	}
	_, err := h.client.GetMe(context.Background(), req)
	return err
}

func TestPublicProcedureNeedsNoToken(t *testing.T) {
	h := newHarness(t, limits{})

	if err := h.login(""); err != nil {
		t.Fatalf("an anonymous call to a public procedure was refused: %v", err)
	}
	if h.stub.calls != 1 {
		t.Fatalf("the call did not reach the handler")
	}
}

func TestProtectedProcedureRequiresAToken(t *testing.T) {
	h := newHarness(t, limits{})

	err := h.getMe("")

	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("code = %v, want unauthenticated", connect.CodeOf(err))
	}
	if h.stub.calls != 0 {
		t.Error("an unauthenticated call reached the handler")
	}
}

func TestProtectedProcedureAcceptsAValidToken(t *testing.T) {
	h := newHarness(t, limits{})

	if err := h.getMe(token(t, nil)); err != nil {
		t.Fatalf("a valid token was refused: %v", err)
	}
	if !h.stub.sawClaims {
		t.Error("verified claims were not put on the context for the handler")
	}
}

func TestProtectedProcedureRejectsBadTokens(t *testing.T) {
	tests := map[string]func(gojwt.MapClaims){
		"expired":        func(c gojwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
		"wrong issuer":   func(c gojwt.MapClaims) { c["iss"] = "https://evil.example" },
		"wrong audience": func(c gojwt.MapClaims) { c["aud"] = "someone-else" },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, limits{})

			err := h.getMe(token(t, mutate))

			if connect.CodeOf(err) != connect.CodeUnauthenticated {
				t.Fatalf("code = %v, want unauthenticated", connect.CodeOf(err))
			}
			if h.stub.calls != 0 {
				t.Error("a call with a bad token reached the handler")
			}
		})
	}

	t.Run("garbage", func(t *testing.T) {
		h := newHarness(t, limits{})

		if code := connect.CodeOf(h.getMe("not-a-token")); code != connect.CodeUnauthenticated {
			t.Fatalf("code = %v, want unauthenticated", code)
		}
	})
}

// A bad token on a public procedure is not a reason to refuse — the procedure
// did not need one. It is a reason not to credit the caller with an identity
// they failed to prove.
func TestBadTokenOnAPublicProcedureIsIgnored(t *testing.T) {
	h := newHarness(t, limits{})

	if err := h.login("not-a-token"); err != nil {
		t.Fatalf("a public call with a bad token was refused: %v", err)
	}
	if h.stub.sawClaims {
		t.Error("an unverified token produced claims on the context")
	}
}

// Sign-in is on the tight budget, so a normal browsing budget must not apply.
func TestSensitiveProcedureUsesTheTightBudget(t *testing.T) {
	h := newHarness(t, limits{standard: 1000, sensitive: 3})

	for i := range 3 {
		if err := h.login(""); err != nil {
			t.Fatalf("login %d was refused inside the budget: %v", i+1, err)
		}
	}

	err := h.login("")
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("code = %v, want resource_exhausted", connect.CodeOf(err))
	}
}

// The two budgets are separate: grinding on login must not lock a signed-in
// user out of ordinary calls.
func TestBudgetsAreIndependent(t *testing.T) {
	h := newHarness(t, limits{standard: 100, sensitive: 2})

	for range 2 {
		_ = h.login("")
	}
	if connect.CodeOf(h.login("")) != connect.CodeResourceExhausted {
		t.Fatal("the sensitive budget was not exhausted")
	}

	if err := h.getMe(token(t, nil)); err != nil {
		t.Fatalf("the standard budget was consumed by sensitive traffic: %v", err)
	}
}

// An authenticated caller is limited by account, not by address: otherwise
// everyone behind one NAT shares a budget, and one user on a phone gets a fresh
// one every time their address changes.
func TestAuthenticatedCallersAreLimitedIndependently(t *testing.T) {
	h := newHarness(t, limits{standard: 2})

	first := token(t, nil)
	second := token(t, nil)

	for range 2 {
		if err := h.getMe(first); err != nil {
			t.Fatalf("a call inside the budget was refused: %v", err)
		}
	}
	if connect.CodeOf(h.getMe(first)) != connect.CodeResourceExhausted {
		t.Fatal("the first caller was not throttled")
	}

	// Same address, different account.
	if err := h.getMe(second); err != nil {
		t.Fatalf("a second account was throttled by the first one's traffic: %v", err)
	}
}

func TestNewValidatesItsDependencies(t *testing.T) {
	limiter := ratelimit.New(ratelimit.Config{PerMinute: 10})

	cases := map[string]guard.Config{
		"no verifier": {Standard: limiter, Sensitive: limiter, Logger: logger.Discard()},
		"no limiters": {Logger: logger.Discard()},
		"no logger":   {Standard: limiter, Sensitive: limiter},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := guard.New(cfg); err == nil {
				t.Fatal("an invalid configuration was accepted")
			}
		})
	}
}
