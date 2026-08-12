package oauth

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bvivg/axon/core/shared/pkg/authn"

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

	Production bool
}

type Registry struct {
	providers map[domain.Provider]Provider
}

func NewRegistry(cfg RegistryConfig) (*Registry, error) {
	if strings.TrimSpace(cfg.RedirectBaseURL) == "" {
		return nil, errors.New("oauth: redirect base URL is required")
	}

	if cfg.FakeEnabled && cfg.Production {
		return nil, errors.New("oauth: the fake provider cannot be enabled in production")
	}

	base := strings.TrimSuffix(cfg.RedirectBaseURL, "/")
	providers := make(map[domain.Provider]Provider)

	if cfg.Google.configured() {
		providers[domain.ProviderGoogle] = newGoogle(
			cfg.Google.ClientID, cfg.Google.ClientSecret,
			base+"/"+domain.ProviderGoogle.String(),
		)
	}

	if cfg.GitHub.configured() {
		providers[domain.ProviderGitHub] = newGitHub(
			cfg.GitHub.ClientID, cfg.GitHub.ClientSecret,
			base+"/"+domain.ProviderGitHub.String(),
		)
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
		providers[domain.ProviderFake] = newFake(
			cfg.FakeAuthorizeURL,
			base+"/"+domain.ProviderFake.String(),
		)
	}

	return &Registry{providers: providers}, nil
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
