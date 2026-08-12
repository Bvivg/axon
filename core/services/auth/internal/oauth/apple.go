package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

var (
	appleAuthorizeURL = "https://appleid.apple.com/auth/authorize"
	appleExchangeURL  = "https://appleid.apple.com/auth/token"
	appleKeysURL      = "https://appleid.apple.com/auth/keys"
)

const appleIssuer = "https://appleid.apple.com"

const AppleCallbackPath = "/auth/apple/callback"

const appleScope = "name email"

type AppleConfig struct {
	ClientID string

	TeamID string

	KeyID string

	PrivateKey string
}

func (c AppleConfig) configured() bool {
	return strings.TrimSpace(c.ClientID) != "" &&
		strings.TrimSpace(c.TeamID) != "" &&
		strings.TrimSpace(c.KeyID) != "" &&
		strings.TrimSpace(c.PrivateKey) != ""
}

type appleProvider struct {
	clientID    string
	redirectURL string

	secret *appleClientSecret

	keys authn.KeySource

	client *http.Client
	now    func() time.Time
}

func newApple(cfg AppleConfig, redirectURL string, keys authn.KeySource) (Provider, error) {
	if keys == nil {
		return nil, errors.New("oauth: apple: a key source is required to verify id tokens")
	}

	secret, err := newAppleClientSecret(cfg)
	if err != nil {
		return nil, err
	}

	return &appleProvider{
		clientID:    strings.TrimSpace(cfg.ClientID),
		redirectURL: redirectURL,
		secret:      secret,
		keys:        keys,
		client:      &http.Client{Timeout: exchangeTimeout},
		now:         time.Now,
	}, nil
}

func (p *appleProvider) ID() domain.Provider { return domain.ProviderApple }

func (p *appleProvider) AuthorizationURL(state, challenge string) string {
	query := url.Values{
		"client_id":     {p.clientID},
		"redirect_uri":  {p.redirectURL},
		"response_type": {"code"},
		"response_mode": {"form_post"},
		"scope":         {appleScope},
		"state":         {state},

		"code_challenge":        {challenge},
		"code_challenge_method": {Method},
	}
	return appleAuthorizeURL + "?" + query.Encode()
}

func (p *appleProvider) Exchange(ctx context.Context, code, verifier string) (domain.ProviderProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, exchangeTimeout)
	defer cancel()

	secret, err := p.secret.value()
	if err != nil {
		return domain.ProviderProfile{}, err
	}

	idToken, err := p.redeem(ctx, code, verifier, secret)
	if err != nil {
		return domain.ProviderProfile{}, err
	}

	profile, err := p.profileFromIDToken(idToken)
	if err != nil {
		return domain.ProviderProfile{}, err
	}

	if err := validateProfile(profile); err != nil {
		return domain.ProviderProfile{}, fmt.Errorf("oauth: apple: %w", err)
	}

	return profile, nil
}

type appleTokenResponse struct {
	IDToken          string `json:"id_token"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (p *appleProvider) redeem(ctx context.Context, code, verifier, secret string) (string, error) {
	form := url.Values{
		"client_id":     {p.clientID},
		"client_secret": {secret},
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {p.redirectURL},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, appleExchangeURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("oauth: apple: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth: apple: exchange code: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body appleTokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxProfileBytes)).Decode(&body); err != nil {
		return "", fmt.Errorf("oauth: apple: decode token response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("oauth: apple: token endpoint answered %s: %s",
			resp.Status, body.Error)
	}

	if body.IDToken == "" {
		return "", errors.New("oauth: apple: the token response carried no id_token")
	}
	return body.IDToken, nil
}

func (p *appleProvider) profileFromIDToken(raw string) (domain.ProviderProfile, error) {
	parsed, err := jwt.Parse(raw, p.keyFunc,

		jwt.WithValidMethods([]string{authn.Algorithm}),
		jwt.WithIssuer(appleIssuer),

		jwt.WithAudience(p.clientID),
		jwt.WithLeeway(authn.Leeway),
		jwt.WithTimeFunc(p.now),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return domain.ProviderProfile{}, fmt.Errorf("oauth: apple: verify id_token: %w", err)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return domain.ProviderProfile{}, errors.New("oauth: apple: id_token claims are not an object")
	}

	subject, err := claims.GetSubject()
	if err != nil {
		return domain.ProviderProfile{}, fmt.Errorf("oauth: apple: id_token subject: %w", err)
	}

	return domain.ProviderProfile{
		ProviderUserID: subject,
		Email:          stringClaim(claims, "email"),
		EmailVerified:  appleBool(claims, "email_verified"),

		IsPrivateEmail: appleBool(claims, "is_private_email"),
	}, nil
}

func (p *appleProvider) keyFunc(token *jwt.Token) (any, error) {
	keyID, ok := token.Header["kid"].(string)
	if !ok || keyID == "" {
		return nil, errors.New("oauth: apple: id_token names no key")
	}

	key, ok := p.keys.PublicKey(keyID)
	if !ok {
		return nil, fmt.Errorf("oauth: apple: id_token signed by unknown key %q", keyID)
	}
	return key, nil
}

func appleBool(claims jwt.MapClaims, key string) bool {
	switch v := claims[key].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	default:
		return false
	}
}

func stringClaim(claims jwt.MapClaims, key string) string {
	s, _ := claims[key].(string)
	return s
}

func appleRedirectURL(base string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("oauth: apple: %q is not an absolute redirect base URL", base)
	}
	return parsed.Scheme + "://" + parsed.Host + AppleCallbackPath, nil
}
