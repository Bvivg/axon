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

const FakeAuthorizePath = "/oauth/fake/authorize"

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

		verified := query.Get("email_verified") != "false"

		code, err := encodeFakeCode(domain.ProviderProfile{
			ProviderUserID: subject,
			Email:          email,
			EmailVerified:  verified,
			DisplayName:    query.Get("name"),
			AvatarURL:      query.Get("avatar_url"),
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

func encodeFakeCode(p domain.ProviderProfile) (string, error) {
	encoded, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("oauth: fake: encode code: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

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
