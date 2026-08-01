// Package oauth implements sign-in through external identity providers.
//
// It owns the authorization code flow and nothing else: the service layer
// decides what a verified identity means for an account, and this package
// decides only what the provider said. Nothing here touches the database.
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

// Provider is one external identity provider.
type Provider interface {
	// ID is the provider this implements.
	ID() domain.Provider

	// AuthorizationURL is where the browser is sent to obtain consent.
	AuthorizationURL(state, challenge string) string

	// Exchange redeems an authorization code for the profile behind it.
	Exchange(ctx context.Context, code, verifier string) (domain.ProviderProfile, error)
}

// exchangeTimeout bounds the round trips to a provider. A provider having a bad
// day must fail a sign-in, not tie up a connection until the client gives up.
const exchangeTimeout = 10 * time.Second

// fetchProfile reads the signed-in identity from a provider's API, using a
// client that already carries the access token.
type fetchProfile func(ctx context.Context, client *http.Client) (domain.ProviderProfile, error)

// httpProvider is the shape every provider in this package takes: an OAuth2
// exchange followed by one or more calls to the provider's own API.
//
// Google and GitHub differ only in that second half, which is why they share
// this and supply their own fetchProfile rather than each reimplementing the
// flow. Apple will not fit here — its client secret is a signed assertion and
// its profile arrives inside the token response — and that is a reason to give
// it its own type, not to generalise this one until it stops being readable.
type httpProvider struct {
	id      domain.Provider
	cfg     *oauth2.Config
	profile fetchProfile
}

func (p *httpProvider) ID() domain.Provider { return p.id }

func (p *httpProvider) AuthorizationURL(state, challenge string) string {
	return p.cfg.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", Method),
	)
}

func (p *httpProvider) Exchange(ctx context.Context, code, verifier string) (domain.ProviderProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, exchangeTimeout)
	defer cancel()

	token, err := p.cfg.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		// The provider's message can quote the code back. It goes no further
		// than this wrap, which the service layer logs and does not return.
		return domain.ProviderProfile{}, fmt.Errorf("oauth: %s: exchange code: %w", p.id, err)
	}

	profile, err := p.profile(ctx, p.cfg.Client(ctx, token))
	if err != nil {
		return domain.ProviderProfile{}, fmt.Errorf("oauth: %s: fetch profile: %w", p.id, err)
	}

	if err := validateProfile(profile); err != nil {
		return domain.ProviderProfile{}, fmt.Errorf("oauth: %s: %w", p.id, err)
	}

	return profile, nil
}

// validateProfile refuses a profile that cannot safely become an account.
//
// Both checks are load-bearing. The subject identifier is the primary key of the
// link, so without it there is nothing stable to recognise the person by next
// time. The verified flag is what makes matching on email safe at all: an
// unverified address means the provider is repeating a claim, not vouching for
// it, and acting on it hands over any account whose owner uses that address.
func validateProfile(p domain.ProviderProfile) error {
	if strings.TrimSpace(p.ProviderUserID) == "" || strings.TrimSpace(p.Email) == "" {
		return domain.ErrOauthProfileIncomplete
	}
	if !p.EmailVerified {
		return domain.ErrOauthEmailUnverified
	}
	return nil
}

// maxProfileBytes bounds a provider response. A provider is not trusted to send
// something a decoder would hold in memory, however unlikely a hostile response
// from Google is.
const maxProfileBytes = 1 << 20

// get performs a GET with the token-carrying client and decodes the result.
//
// The status is checked explicitly: a provider answering 200 with an error body
// is common enough that decoding straight into the profile would silently
// produce an empty one.
func get(ctx context.Context, client *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider answered %s", resp.Status)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxProfileBytes)).Decode(v); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
