package oauth

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	authjwt "github.com/bvivg/axon/core/services/auth/internal/jwt"
)

// testAppleJWKSKeyID is the kid Apple's stand-in publishes and signs with.
const testAppleJWKSKeyID = "apple-signing-key-1"

// testRSAKey stands in for the key Apple signs id_tokens with. Generated once
// per test binary: 2048-bit generation is slow enough to dominate the suite if
// it happened per test.
var testRSAKey = sync.OnceValue(func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
})

// rsaPEM renders that key as a PKCS#8 PEM, for the case where an operator
// supplies the wrong kind of key file.
func rsaPEM(t *testing.T) string {
	t.Helper()

	der, err := x509.MarshalPKCS8PrivateKey(testRSAKey())
	if err != nil {
		t.Fatalf("marshal RSA key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// appleStub stands in for Apple: a token endpoint and a key set, over real HTTP
// with real signatures.
//
// It is not a mock of the Provider interface. What is under test is the half of
// the conversation this service holds — a form-encoded token request carrying a
// signed assertion, and an id_token verified against a fetched key set — and a
// mock of Provider would exercise none of it.
type appleStub struct {
	server *httptest.Server

	// claims is what the id_token carries. A test changes one and repeats the
	// exchange to drive a particular refusal.
	claims jwt.MapClaims

	// signWith overrides the key the id_token is signed with, and keyID the kid
	// it names: the two ways a token can fail to be Apple's.
	signWith *rsa.PrivateKey
	keyID    string

	// status overrides the token endpoint's answer.
	status int

	// clientKey is the .p8 the provider under test signs its client secret with,
	// so a test can verify the assertion Apple would have received.
	clientKey *ecdsa.PrivateKey

	mu   sync.Mutex
	form url.Values
}

func newAppleStub(t *testing.T) *appleStub {
	t.Helper()

	now := time.Now()
	stub := &appleStub{
		claims: jwt.MapClaims{
			"iss":   appleIssuer,
			"aud":   testAppleClientID,
			"sub":   "001234.fedcba9876543210.1234",
			"email": "person@example.com",
			// Strings, not booleans: Apple has sent these as strings for years,
			// and a verifier that only understands booleans reads "true" as
			// false and refuses every sign-in.
			"email_verified":   "true",
			"is_private_email": "false",
			"iat":              now.Add(-time.Minute).Unix(),
			"exp":              now.Add(time.Hour).Unix(),
		},
		keyID: testAppleJWKSKeyID,
	}

	keySet, err := authjwt.NewKeySet(authjwt.PrivateKey{ID: testAppleJWKSKeyID, Key: testRSAKey()})
	if err != nil {
		t.Fatalf("build the stand-in key set: %v", err)
	}
	document, err := keySet.JWKS()
	if err != nil {
		t.Fatalf("render the stand-in JWKS: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("POST /auth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}

		stub.mu.Lock()
		stub.form = r.PostForm
		stub.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		if stub.status != 0 && stub.status != http.StatusOK {
			w.WriteHeader(stub.status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_client"})
			return
		}

		key := stub.signWith
		if key == nil {
			key = testRSAKey()
		}

		token := jwt.NewWithClaims(jwt.SigningMethodRS256, stub.claims)
		token.Header["kid"] = stub.keyID

		signed, err := token.SignedString(key)
		if err != nil {
			http.Error(w, "could not sign the id_token", http.StatusInternalServerError)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "stub-access-token",
			"token_type":   "Bearer",
			"id_token":     signed,
		})
	})

	mux.HandleFunc("GET /auth/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(document)
	})

	stub.server = httptest.NewServer(mux)
	t.Cleanup(stub.server.Close)

	originalToken, originalKeys := appleExchangeURL, appleKeysURL
	appleExchangeURL = stub.server.URL + "/auth/token"
	appleKeysURL = stub.server.URL + "/auth/keys"
	t.Cleanup(func() {
		appleExchangeURL, appleKeysURL = originalToken, originalKeys
	})

	return stub
}

// provider builds the provider under test against this stub, with the real JWKS
// cache pointed at the stub's key set — so the fetch and the RFC 7517 parsing
// are part of what is being tested, not stubbed out of it.
func (s *appleStub) provider(t *testing.T) *appleProvider {
	t.Helper()

	cfg, key := appleTestConfig(t)
	s.clientKey = key

	keys, err := authn.NewCache(authn.JWKSConfig{URL: appleKeysURL, Logger: logger.Discard()})
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	provider, err := newApple(cfg, testAppleRedirectURL, keys)
	if err != nil {
		t.Fatalf("newApple: %v", err)
	}

	apple, ok := provider.(*appleProvider)
	if !ok {
		t.Fatalf("newApple returned %T", provider)
	}
	return apple
}

// sentForm is the token request the provider made.
func (s *appleStub) sentForm() url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.form
}

const testAppleRedirectURL = "https://axon.test" + AppleCallbackPath

// Apple's callback is a form POST, which is why it returns to a server route
// rather than to the page every other provider comes back to. The scope is what
// makes it so, and PKCE goes along as it does everywhere else.
func TestAppleAuthorizationURLAsksForFormPost(t *testing.T) {
	stub := newAppleStub(t)

	raw := stub.provider(t).AuthorizationURL("a-state", "a-challenge")

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != appleAuthorizeURL {
		t.Errorf("authorization endpoint = %q", raw)
	}

	for param, want := range map[string]string{
		"client_id":     testAppleClientID,
		"redirect_uri":  testAppleRedirectURL,
		"response_type": "code",
		// Without form_post Apple never sends the name, and with the name scope
		// it refuses anything else.
		"response_mode":         "form_post",
		"scope":                 appleScope,
		"state":                 "a-state",
		"code_challenge":        "a-challenge",
		"code_challenge_method": Method,
	} {
		if got := parsed.Query().Get(param); got != want {
			t.Errorf("%s = %q, want %q", param, got, want)
		}
	}
}

// The whole Apple exchange: a signed assertion in place of a client secret, a
// code redeemed over a form POST, and a profile that exists only inside the
// id_token.
func TestAppleReadsTheProfileFromTheIDToken(t *testing.T) {
	stub := newAppleStub(t)
	stub.claims["is_private_email"] = "true"

	provider := stub.provider(t)

	profile, err := provider.Exchange(t.Context(), "apple-code-1", "a-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	if profile.ProviderUserID != "001234.fedcba9876543210.1234" {
		t.Errorf("subject = %q, want the sub claim", profile.ProviderUserID)
	}
	if profile.Email != "person@example.com" {
		t.Errorf("email = %q", profile.Email)
	}
	if !profile.EmailVerified {
		t.Error(`email_verified arrived as the string "true" and was read as false`)
	}
	if !profile.IsPrivateEmail {
		t.Error("is_private_email was not carried through")
	}
	// Apple never tells this service the name — it goes to whoever received the
	// callback, which is the client, and comes back on the request instead.
	if profile.DisplayName != "" {
		t.Errorf("display name = %q, want none: the id_token does not carry one", profile.DisplayName)
	}

	form := stub.sentForm()
	for param, want := range map[string]string{
		"client_id":     testAppleClientID,
		"code":          "apple-code-1",
		"code_verifier": "a-verifier",
		"grant_type":    "authorization_code",
		"redirect_uri":  testAppleRedirectURL,
	} {
		if got := form.Get(param); got != want {
			t.Errorf("token request %s = %q, want %q", param, got, want)
		}
	}

	// The client secret Apple received has to verify against the .p8 this
	// deployment holds, or every exchange fails with invalid_client.
	assertion, err := jwt.Parse(form.Get("client_secret"),
		func(*jwt.Token) (any, error) { return &stub.clientKey.PublicKey, nil },
		jwt.WithValidMethods([]string{"ES256"}),
		jwt.WithIssuer(testAppleTeamID),
		jwt.WithAudience(appleIssuer),
	)
	if err != nil {
		t.Fatalf("the client secret does not verify: %v", err)
	}
	if subject, err := assertion.Claims.GetSubject(); err != nil || subject != testAppleClientID {
		t.Errorf("client secret sub = %q, want the Services ID", subject)
	}
}

// The same claims as booleans, which is the other form Apple sends.
func TestAppleAcceptsBooleanClaims(t *testing.T) {
	stub := newAppleStub(t)
	stub.claims["email_verified"] = true
	stub.claims["is_private_email"] = true

	profile, err := stub.provider(t).Exchange(t.Context(), "apple-code-1", "a-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if !profile.EmailVerified || !profile.IsPrivateEmail {
		t.Errorf("boolean claims were not read: %+v", profile)
	}
}

// Accounts are matched by address, so an address the provider has not verified
// is an account takeover waiting for the right victim. Apple is no exception,
// however unlikely it is to send one.
func TestAppleRefusesAnUnverifiedAddress(t *testing.T) {
	for name, claim := range map[string]any{
		`the string "false"`: "false",
		"the boolean false":  false,
		"a missing claim":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			stub := newAppleStub(t)
			if claim == nil {
				delete(stub.claims, "email_verified")
			} else {
				stub.claims["email_verified"] = claim
			}

			_, err := stub.provider(t).Exchange(t.Context(), "apple-code-1", "a-verifier")
			if !errors.Is(err, domain.ErrOauthEmailUnverified) {
				t.Fatalf("err = %v, want ErrOauthEmailUnverified", err)
			}
		})
	}
}

// An id_token is the entire trust decision here: nothing else in the exchange is
// authenticated. Every one of these must end the sign-in rather than produce a
// profile.
func TestAppleRefusesAnIDTokenItCannotTrust(t *testing.T) {
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate another key: %v", err)
	}

	for name, break_ := range map[string]func(*appleStub){
		// The signature is the point. A token signed by anyone else is a forgery
		// however well-formed its claims are.
		"signed by another key": func(s *appleStub) { s.signWith = otherKey },

		// A kid nobody published cannot be resolved, and guessing is not an
		// option.
		"signed by a key that is not published": func(s *appleStub) { s.keyID = "some-other-key" },

		// An id_token Apple issued to a different application. Accepting it
		// would let another Services ID sign people in here.
		"issued for another audience": func(s *appleStub) { s.claims["aud"] = "com.example.someone-else" },

		"issued by someone other than Apple": func(s *appleStub) { s.claims["iss"] = "https://not-apple.example" },

		"expired": func(s *appleStub) {
			s.claims["exp"] = time.Now().Add(-time.Hour).Unix()
		},

		"never expires": func(s *appleStub) { delete(s.claims, "exp") },
	} {
		t.Run(name, func(t *testing.T) {
			stub := newAppleStub(t)
			break_(stub)

			profile, err := stub.provider(t).Exchange(t.Context(), "apple-code-1", "a-verifier")
			if err == nil {
				t.Fatalf("the token was accepted: %+v", profile)
			}
			if profile.ProviderUserID != "" || profile.Email != "" {
				t.Errorf("a rejected token still produced a profile: %+v", profile)
			}
		})
	}
}

// Without an address there is nothing to match the account on, and the sign-in
// cannot be completed either way.
func TestAppleRefusesAProfileWithNoAddress(t *testing.T) {
	stub := newAppleStub(t)
	delete(stub.claims, "email")

	_, err := stub.provider(t).Exchange(t.Context(), "apple-code-1", "a-verifier")
	if !errors.Is(err, domain.ErrOauthProfileIncomplete) {
		t.Fatalf("err = %v, want ErrOauthProfileIncomplete", err)
	}
}

// invalid_client is what Apple answers when the assertion or its key is wrong.
// It is an operator's problem and invisible unless it is carried out.
func TestAppleReportsARejectedExchange(t *testing.T) {
	stub := newAppleStub(t)
	stub.status = http.StatusBadRequest

	_, err := stub.provider(t).Exchange(t.Context(), "apple-code-1", "a-verifier")
	if err == nil {
		t.Fatal("a rejected exchange was treated as success")
	}
	if !strings.Contains(err.Error(), "invalid_client") {
		t.Errorf("the error does not carry Apple's reason: %v", err)
	}
}

// Four values, not two, and a partly-filled entry counts as absent for the same
// reason it does everywhere else: a provider that looks available and fails at
// the exchange breaks in the middle of somebody's sign-in.
func TestAppleIsRegisteredOnlyWithAllFourCredentials(t *testing.T) {
	full, _ := appleTestConfig(t)

	for name, cfg := range map[string]AppleConfig{
		"nothing":      {},
		"no client id": {TeamID: full.TeamID, KeyID: full.KeyID, PrivateKey: full.PrivateKey},
		"no team id":   {ClientID: full.ClientID, KeyID: full.KeyID, PrivateKey: full.PrivateKey},
		"no key id":    {ClientID: full.ClientID, TeamID: full.TeamID, PrivateKey: full.PrivateKey},
		"no private key": {
			ClientID: full.ClientID, TeamID: full.TeamID, KeyID: full.KeyID,
		},
	} {
		t.Run(name, func(t *testing.T) {
			registry, err := NewRegistry(RegistryConfig{
				RedirectBaseURL: "http://localhost:3000/auth/callback",
				Apple:           cfg,
				Logger:          logger.Discard(),
			})
			if err != nil {
				t.Fatalf("NewRegistry: %v", err)
			}
			if _, err := registry.Get(domain.ProviderApple); !errors.Is(err, domain.ErrProviderUnsupported) {
				t.Fatalf("Get(apple) = %v, want ErrProviderUnsupported", err)
			}
		})
	}
}

// With all four, Apple is a provider like any other — except for where it sends
// the browser back to, which is the whole reason it needed its own type.
func TestAppleIsAvailableWithAllFourCredentials(t *testing.T) {
	cfg, _ := appleTestConfig(t)

	registry, err := NewRegistry(RegistryConfig{
		RedirectBaseURL: "http://localhost:3000/auth/callback",
		Apple:           cfg,
		Logger:          logger.Discard(),
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	provider, err := registry.Get(domain.ProviderApple)
	if err != nil {
		t.Fatalf("Get(apple): %v", err)
	}
	if provider.ID() != domain.ProviderApple {
		t.Errorf("provider id = %q", provider.ID())
	}

	available := registry.Available()
	if len(available) != 1 || available[0] != domain.ProviderApple {
		t.Errorf("Available() = %v, want [apple]", available)
	}

	// Not /auth/callback/apple: a route handler on the page's own path would
	// catch the redirect it issues and bounce the flow off itself.
	authorizeURL := provider.AuthorizationURL("a-state", "a-challenge")
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if got := parsed.Query().Get("redirect_uri"); got != "http://localhost:3000"+AppleCallbackPath {
		t.Errorf("redirect_uri = %q", got)
	}
}

// The key cache reports failed refreshes, and a provider that cannot say when it
// has stopped being able to verify tokens is worse than one that refuses to
// start.
func TestAppleNeedsALoggerForItsKeyCache(t *testing.T) {
	cfg, _ := appleTestConfig(t)

	_, err := NewRegistry(RegistryConfig{
		RedirectBaseURL: "http://localhost:3000/auth/callback",
		Apple:           cfg,
	})
	if err == nil {
		t.Fatal("Apple was registered with no logger")
	}
}

// A base URL that is not absolute has no origin to hang Apple's route off, and
// guessing one would send people somewhere nobody registered with Apple.
func TestAppleNeedsAnAbsoluteRedirectBase(t *testing.T) {
	cfg, _ := appleTestConfig(t)

	_, err := NewRegistry(RegistryConfig{
		RedirectBaseURL: "/auth/callback",
		Apple:           cfg,
		Logger:          logger.Discard(),
	})
	if err == nil {
		t.Fatal("a relative redirect base was accepted for Apple")
	}
}
