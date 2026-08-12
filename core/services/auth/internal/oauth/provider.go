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

type Provider interface {
	ID() domain.Provider

	AuthorizationURL(state, challenge string) string

	Exchange(ctx context.Context, code, verifier string) (domain.ProviderProfile, error)
}

const exchangeTimeout = 10 * time.Second

type fetchProfile func(ctx context.Context, client *http.Client) (domain.ProviderProfile, error)

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

func validateProfile(p domain.ProviderProfile) error {
	if strings.TrimSpace(p.ProviderUserID) == "" || strings.TrimSpace(p.Email) == "" {
		return domain.ErrOauthProfileIncomplete
	}
	if !p.EmailVerified {
		return domain.ErrOauthEmailUnverified
	}
	return nil
}

const maxProfileBytes = 1 << 20

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
