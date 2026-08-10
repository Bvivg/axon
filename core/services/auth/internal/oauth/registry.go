package oauth

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bvivg/axon/core/shared/pkg/authn"

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

	// Apple carries four values instead of two, because its client secret is
	// signed rather than issued. It returns the browser to its own route: see
	// AppleCallbackPath.
	Apple AppleConfig

	// Logger is what the Apple key cache reports failed refreshes on. It is
	// required only when Apple is configured, since nothing else here logs.
	Logger *slog.Logger

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

// newAppleFromConfig assembles Apple together with the key cache its id_token
// verification reads from.
//
// The cache is not primed here on purpose. Fetching Apple's key set at startup
// would make this service's ability to start depend on reaching appleid.apple.com,
// and the cache already fetches on the first token that names a key it has not
// seen — which is the first Apple sign-in.
func newAppleFromConfig(cfg RegistryConfig) (Provider, error) {
	if cfg.Logger == nil {
		return nil, errors.New("oauth: apple needs a logger for its key cache")
	}

	redirectURL, err := appleRedirectURL(cfg.RedirectBaseURL)
	if err != nil {
		return nil, err
	}

	// The shared JWKS cache rather than a second one written here: it already
	// knows to collapse concurrent fetches, to re-fetch once for an unknown key
	// id behind a cooldown, and to keep serving the previous keys when a fetch
	// fails. A second copy of that would drift from the first.
	keys, err := authn.NewCache(authn.JWKSConfig{
		URL:    appleKeysURL,
		Logger: cfg.Logger.With("component", "apple_jwks"),
	})
	if err != nil {
		return nil, fmt.Errorf("oauth: apple: key cache: %w", err)
	}

	return newApple(cfg.Apple, redirectURL, keys)
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
