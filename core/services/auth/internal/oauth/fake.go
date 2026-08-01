package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

// FakeAuthorizePath is where the fake provider's consent screen would be, if it
// had one. It is mounted on the auth service's public listener.
const FakeAuthorizePath = "/oauth/fake/authorize"

// fakeProvider stands in for a real identity provider during development and
// tests.
//
// It is not a mock of this package's own interface: it drives the same
// authorization code flow through the same code paths, over real HTTP, with a
// real redirect. State consumption, PKCE, the return_to allow-list and the
// account-linking rules are all exercised — everything except a third party
// being involved. That is the difference between a test that proves the flow
// works and one that proves the mock was called.
//
// The code carries the identity, so no state is kept between the authorize
// request and the exchange. That is not how a real provider works, and it does
// not need to be: what is under test is this service's half of the conversation.
type fakeProvider struct {
	authorizeURL string
	redirectURL  string
}

func newFake(authorizeURL, redirectURL string) Provider {
	return &fakeProvider{authorizeURL: authorizeURL, redirectURL: redirectURL}
}

func (p *fakeProvider) ID() domain.Provider { return domain.ProviderFake }

func (p *fakeProvider) AuthorizationURL(state, challenge string) string {
	query := url.Values{
		"state":                 {state},
		"redirect_uri":          {p.redirectURL},
		"code_challenge":        {challenge},
		"code_challenge_method": {Method},
	}
	return p.authorizeURL + "?" + query.Encode()
}

// Exchange decodes the identity the authorize handler put in the code.
//
// The verifier is accepted and ignored: there is no third party to have bound it
// to a challenge, and pretending otherwise would only test this file against
// itself. PKCE is still exercised where it is real — the verifier is generated,
// stored with the state and sent — and the real providers do the binding.
func (p *fakeProvider) Exchange(_ context.Context, code, _ string) (domain.ProviderProfile, error) {
	profile, err := decodeFakeCode(code)
	if err != nil {
		return domain.ProviderProfile{}, err
	}

	if err := validateProfile(profile); err != nil {
		return domain.ProviderProfile{}, fmt.Errorf("oauth: fake: %w", err)
	}
	return profile, nil
}

// FakeAuthorizeHandler answers the fake provider's authorization request the way
// a provider would: it redirects straight back to the client with a code and the
// caller's own state, with no consent screen in between.
//
// The identity is taken from the query, so a test can sign in as anyone:
//
//	/oauth/fake/authorize?...&email=someone@example.test&sub=fake-1
//
// Both default to a generated value, so the simplest possible request still
// produces a usable, distinct identity.
func FakeAuthorizeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		redirectURI := query.Get("redirect_uri")
		if redirectURI == "" {
			http.Error(w, "redirect_uri is required", http.StatusBadRequest)
			return
		}

		target, err := url.Parse(redirectURI)
		if err != nil {
			http.Error(w, "redirect_uri is not a URL", http.StatusBadRequest)
			return
		}

		subject := query.Get("sub")
		email := query.Get("email")
		if subject == "" {
			subject = "fake-" + strings.TrimSpace(query.Get("state"))
		}
		if email == "" {
			email = subject + "@fake.axon.test"
		}

		// Unverified on request, so a test can drive the path where a provider
		// declines to vouch for the address.
		verified := query.Get("email_verified") != "false"

		code, err := encodeFakeCode(domain.ProviderProfile{
			ProviderUserID: subject,
			Email:          email,
			EmailVerified:  verified,
			DisplayName:    query.Get("name"),
		})
		if err != nil {
			http.Error(w, "could not mint a code", http.StatusInternalServerError)
			return
		}

		params := target.Query()
		params.Set("code", code)
		params.Set("state", query.Get("state"))
		target.RawQuery = params.Encode()

		http.Redirect(w, r, target.String(), http.StatusFound)
	})
}

// encodeFakeCode packs an identity into an authorization code.
func encodeFakeCode(p domain.ProviderProfile) (string, error) {
	encoded, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("oauth: fake: encode code: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

// decodeFakeCode unpacks one, treating anything unreadable as a bad code rather
// than as an internal failure.
func decodeFakeCode(code string) (domain.ProviderProfile, error) {
	raw, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		return domain.ProviderProfile{}, fmt.Errorf("oauth: fake: %w", domain.ErrOauthProfileIncomplete)
	}

	var profile domain.ProviderProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return domain.ProviderProfile{}, fmt.Errorf("oauth: fake: %w", domain.ErrOauthProfileIncomplete)
	}
	return profile, nil
}
