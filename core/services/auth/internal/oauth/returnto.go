package oauth

import (
	"net/url"
	"strings"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

// ReturnToPolicy decides where a client may be sent once sign-in finishes.
//
// The check is not cosmetic. An attacker who can choose the destination points
// it at a host they control, walks a victim through a genuine sign-in, and
// collects the authorization code from their own server logs. That is why the
// list is exact origins and not a prefix or suffix match: allowing
// "https://axon.example" by prefix also allows "https://axon.example.evil.test".
type ReturnToPolicy struct {
	// allowed holds origins in scheme://host[:port] form, already normalised.
	allowed map[string]struct{}

	// fallback is used when a caller names no destination. It is the first
	// configured origin, so a client that omits return_to still lands somewhere
	// sensible rather than nowhere.
	fallback string
}

// NewReturnToPolicy builds a policy from a list of allowed origins.
//
// An empty list is refused: a policy that allows nothing would break every
// sign-in, and one that allows everything is the vulnerability. Configuration
// has to say which it means.
func NewReturnToPolicy(origins []string) (*ReturnToPolicy, error) {
	allowed := make(map[string]struct{}, len(origins))
	var fallback string

	for _, raw := range origins {
		origin, err := normaliseOrigin(raw)
		if err != nil {
			return nil, err
		}
		if fallback == "" {
			fallback = origin
		}
		allowed[origin] = struct{}{}
	}

	if len(allowed) == 0 {
		return nil, domain.ErrOauthReturnToNotAllowed
	}

	return &ReturnToPolicy{allowed: allowed, fallback: fallback}, nil
}

// Resolve validates a requested destination, returning the one to use.
//
// An empty request is not an error — most sign-ins do not care — and resolves to
// the fallback.
func (p *ReturnToPolicy) Resolve(returnTo string) (string, error) {
	if strings.TrimSpace(returnTo) == "" {
		return p.fallback, nil
	}

	parsed, err := url.Parse(returnTo)
	if err != nil {
		return "", domain.ErrOauthReturnToNotAllowed
	}

	// A relative URL has no origin to check, so there is nothing to compare
	// against the list. Refusing is the safe reading: "//evil.example/x" parses
	// as a protocol-relative URL that a browser treats as absolute.
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", domain.ErrOauthReturnToNotAllowed
	}

	origin := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
	if _, ok := p.allowed[origin]; !ok {
		return "", domain.ErrOauthReturnToNotAllowed
	}

	return returnTo, nil
}

// normaliseOrigin reduces a configured entry to scheme://host[:port].
func normaliseOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", domain.ErrOauthReturnToNotAllowed
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host), nil
}
