package oauth

import (
	"errors"
	"strings"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

// ProviderConfig is one provider's credentials.
type ProviderConfig struct {
	ClientID     string
	ClientSecret string
}

// configured reports whether an operator actually set this provider up. A
// half-filled entry counts as absent: a provider with an id and no secret fails
// at the exchange, in the middle of someone's sign-in, rather than at startup.
func (c ProviderConfig) configured() bool {
	return strings.TrimSpace(c.ClientID) != "" && strings.TrimSpace(c.ClientSecret) != ""
}

// RegistryConfig describes which providers exist and where they send the browser
// back to.
type RegistryConfig struct {
	// RedirectBaseURL is the client route the provider returns to. The provider
	// name is appended, giving e.g.
	// https://axon.example/auth/callback/google.
	RedirectBaseURL string

	Google ProviderConfig
	GitHub ProviderConfig

	// FakeEnabled turns on the local provider. Refused outside development and
	// test: see NewRegistry.
	FakeEnabled bool

	// FakeAuthorizeURL is where the fake provider's authorize handler is
	// mounted, as a client would reach it.
	FakeAuthorizeURL string

	// Production is true when this is a production deployment.
	Production bool
}

// Registry holds the providers this deployment offers.
//
// Absence is the default. A provider nobody configured is not registered, so
// asking for it fails with ErrProviderUnsupported instead of producing an
// authorization URL with an empty client id — which a provider answers with an
// error page that has nothing to do with the real problem.
type Registry struct {
	providers map[domain.Provider]Provider
}

// NewRegistry builds the registry from configuration.
func NewRegistry(cfg RegistryConfig) (*Registry, error) {
	if strings.TrimSpace(cfg.RedirectBaseURL) == "" {
		return nil, errors.New("oauth: redirect base URL is required")
	}

	// The fake provider accepts any identity presented to it. In production that
	// is not a test convenience, it is an authentication bypass, so it is not a
	// warning or a log line — the service refuses to start.
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

// Get returns a configured provider.
func (r *Registry) Get(id domain.Provider) (Provider, error) {
	provider, ok := r.providers[id]
	if !ok {
		return nil, domain.ErrProviderUnsupported
	}
	return provider, nil
}

// Available lists the providers a client may offer, so a sign-in page can show
// the buttons that will actually work rather than every button imaginable.
func (r *Registry) Available() []domain.Provider {
	// Fixed order, not map order: a list that reshuffles on every restart makes
	// the sign-in page jump around for no reason.
	order := []domain.Provider{domain.ProviderGoogle, domain.ProviderGitHub, domain.ProviderApple, domain.ProviderFake}

	available := make([]domain.Provider, 0, len(r.providers))
	for _, id := range order {
		if _, ok := r.providers[id]; ok {
			available = append(available, id)
		}
	}
	return available
}
