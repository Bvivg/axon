package oauth

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/config"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

type ProviderConfig struct {
	ClientID     string
	ClientSecret string
}

func (c ProviderConfig) configured() bool {
	return strings.TrimSpace(c.ClientID) != "" && strings.TrimSpace(c.ClientSecret) != ""
}

type RegistryConfig struct {
	RedirectBaseURL string

	Google ProviderConfig
	GitHub ProviderConfig

	Apple AppleConfig

	Logger *slog.Logger

	FakeEnabled bool

	FakeAuthorizeURL string

	Environment config.Environment
}

type Registry struct {
	providers map[domain.Provider]Provider
	apps      map[domain.Provider]Provider
}

const AppCallbackSuffix = "/app"

func NewRegistry(cfg RegistryConfig) (*Registry, error) {
	if strings.TrimSpace(cfg.RedirectBaseURL) == "" {
		return nil, errors.New("oauth: redirect base URL is required")
	}

	if cfg.FakeEnabled {
		if err := fakeAllowed(cfg); err != nil {
			return nil, err
		}
	}

	base := strings.TrimSuffix(cfg.RedirectBaseURL, "/")
	providers := make(map[domain.Provider]Provider)
	apps := make(map[domain.Provider]Provider)
	callback := func(id domain.Provider) string { return base + "/" + id.String() }

	if cfg.Google.configured() {
		providers[domain.ProviderGoogle] = newGoogle(cfg.Google.ClientID, cfg.Google.ClientSecret, callback(domain.ProviderGoogle))
		apps[domain.ProviderGoogle] = newGoogle(cfg.Google.ClientID, cfg.Google.ClientSecret, callback(domain.ProviderGoogle)+AppCallbackSuffix)
	}

	if cfg.GitHub.configured() {
		providers[domain.ProviderGitHub] = newGitHub(cfg.GitHub.ClientID, cfg.GitHub.ClientSecret, callback(domain.ProviderGitHub))
		apps[domain.ProviderGitHub] = newGitHub(cfg.GitHub.ClientID, cfg.GitHub.ClientSecret, callback(domain.ProviderGitHub)+AppCallbackSuffix)
	}

	if cfg.Apple.configured() {
		apple, err := newAppleFromConfig(cfg)
		if err != nil {
			return nil, err
		}
		providers[domain.ProviderApple] = apple
	}

	if cfg.FakeEnabled {
		if strings.TrimSpace(cfg.FakeAuthorizeURL) == "" {
			return nil, errors.New("oauth: the fake provider needs its authorize URL")
		}
		providers[domain.ProviderFake] = newFake(cfg.FakeAuthorizeURL, callback(domain.ProviderFake))
		apps[domain.ProviderFake] = newFake(cfg.FakeAuthorizeURL, callback(domain.ProviderFake)+AppCallbackSuffix)
	}

	return &Registry{providers: providers, apps: apps}, nil
}

func fakeAllowed(cfg RegistryConfig) error {
	switch cfg.Environment {
	case config.EnvTest:
		return nil
	case config.EnvDevelopment:
		if isLoopbackURL(cfg.RedirectBaseURL) && isLoopbackURL(cfg.FakeAuthorizeURL) {
			return nil
		}
		return errors.New("oauth: the fake provider needs loopback OAUTH_REDIRECT_BASE_URL and OAUTH_FAKE_AUTHORIZE_URL with APP_ENV=development")
	default:
		return fmt.Errorf("oauth: the fake provider is allowed only with APP_ENV=test or a loopback APP_ENV=development, not %q", cfg.Environment)
	}
}

func isLoopbackURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func newAppleFromConfig(cfg RegistryConfig) (Provider, error) {
	if cfg.Logger == nil {
		return nil, errors.New("oauth: apple needs a logger for its key cache")
	}

	redirectURL, err := appleRedirectURL(cfg.RedirectBaseURL)
	if err != nil {
		return nil, err
	}

	keys, err := authn.NewCache(authn.JWKSConfig{
		URL:    appleKeysURL,
		Logger: cfg.Logger.With("component", "apple_jwks"),
	})
	if err != nil {
		return nil, fmt.Errorf("oauth: apple: key cache: %w", err)
	}

	return newApple(cfg.Apple, redirectURL, keys)
}

func (r *Registry) Get(id domain.Provider) (Provider, error) {
	provider, ok := r.providers[id]
	if !ok {
		return nil, domain.ErrProviderUnsupported
	}
	return provider, nil
}

func (r *Registry) App(id domain.Provider) (Provider, error) {
	provider, ok := r.apps[id]
	if !ok {
		return nil, domain.ErrProviderUnsupported
	}
	return provider, nil
}

func (r *Registry) Available() []domain.Provider {

	order := []domain.Provider{domain.ProviderGoogle, domain.ProviderGitHub, domain.ProviderApple, domain.ProviderFake}

	available := make([]domain.Provider, 0, len(r.providers))
	for _, id := range order {
		if _, ok := r.providers[id]; ok {
			available = append(available, id)
		}
	}
	return available
}
