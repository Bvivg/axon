package oauth

import (
	"net/url"
	"strings"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

type ReturnToPolicy struct {
	allowed map[string]struct{}

	fallback string
}

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

func (p *ReturnToPolicy) Resolve(returnTo string) (string, error) {
	if strings.TrimSpace(returnTo) == "" {
		return p.fallback, nil
	}

	parsed, err := url.Parse(returnTo)
	if err != nil {
		return "", domain.ErrOauthReturnToNotAllowed
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return "", domain.ErrOauthReturnToNotAllowed
	}

	origin := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
	if _, ok := p.allowed[origin]; !ok {
		return "", domain.ErrOauthReturnToNotAllowed
	}

	return returnTo, nil
}

func normaliseOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", domain.ErrOauthReturnToNotAllowed
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host), nil
}
