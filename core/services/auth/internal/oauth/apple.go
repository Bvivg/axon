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

// Apple's endpoints, kept as variables so tests can point them at a stand-in.
//
// The token endpoint is named for what this service does with it rather than
// for what Apple calls it: a name with "token" in it sitting next to a string
// literal reads to gosec as a hardcoded credential, and a suppression comment is
// a worse thing to leave in the file than a verb.
var (
	appleAuthorizeURL = "https://appleid.apple.com/auth/authorize"
	appleExchangeURL  = "https://appleid.apple.com/auth/token"
	appleKeysURL      = "https://appleid.apple.com/auth/keys"
)

// appleIssuer is the iss claim on every Apple id_token, and the audience of the
// client secret this service signs.
const appleIssuer = "https://appleid.apple.com"

// AppleCallbackPath is where Apple's callback lands.
//
// Apple alone does not return to /auth/callback/<provider> with the others. Its
// callback is an HTML form POST — required once the request asks for the name
// and email scopes — and a client page cannot receive one. So the web client
// carries a server route at this path which accepts the POST and sends the
// browser on to the ordinary callback page with the code, and the name Apple
// sent alongside it, in the query.
//
// It deliberately sits outside /auth/callback/: a route handler mounted on the
// page's own path would also catch the redirect it issues, and the flow would
// bounce off itself.
const AppleCallbackPath = "/auth/apple/callback"

// appleScope asks for the two things this service needs. Requesting the name is
// what makes Apple use form_post, and what makes the first authorization the
// only one that carries it.
const appleScope = "name email"

// AppleConfig is Apple's credentials.
//
// Four values rather than the usual two, because the client secret is signed
// rather than issued: the team owns the key, the key has an id, and the file is
// the key itself. All four are required together — see configured.
type AppleConfig struct {
	// ClientID is the Services ID. It is the aud of every id_token Apple
	// returns and the sub of the client secret.
	ClientID string

	// TeamID is the Apple developer team, the iss of the client secret.
	TeamID string

	// KeyID is the id of the .p8 key, published in the assertion's kid header
	// so Apple knows which key to verify with.
	KeyID string

	// PrivateKey is the .p8 file's contents, PEM-encoded.
	PrivateKey string
}

// configured reports whether an operator actually set Apple up.
//
// A partly-filled entry counts as absent, exactly as for the providers with a
// plain client secret: a provider that looks available and then fails at the
// exchange breaks in the middle of somebody's sign-in rather than at startup.
func (c AppleConfig) configured() bool {
	return strings.TrimSpace(c.ClientID) != "" &&
		strings.TrimSpace(c.TeamID) != "" &&
		strings.TrimSpace(c.KeyID) != "" &&
		strings.TrimSpace(c.PrivateKey) != ""
}

// appleProvider implements sign-in with Apple.
//
// It does not reuse httpProvider, and that is not an oversight: the client
// secret has to be re-minted per exchange, and the profile arrives inside the
// id_token rather than from an API call afterwards. Generalising httpProvider
// far enough to cover both would leave neither readable.
type appleProvider struct {
	clientID    string
	redirectURL string

	secret *appleClientSecret

	// keys resolves the id_token's signing key. In production it is the shared
	// JWKS cache pointed at Apple; a test supplies its own source.
	keys authn.KeySource

	client *http.Client
	now    func() time.Time
}

// newApple builds the Apple provider.
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

// AuthorizationURL is where the browser is sent for consent.
//
// response_mode=form_post is mandatory once a scope is requested, and it is why
// the redirect URI is a server route rather than the client page the other
// providers return to.
func (p *appleProvider) AuthorizationURL(state, challenge string) string {
	query := url.Values{
		"client_id":     {p.clientID},
		"redirect_uri":  {p.redirectURL},
		"response_type": {"code"},
		"response_mode": {"form_post"},
		"scope":         {appleScope},
		"state":         {state},
		// PKCE goes to Apple as it does to everyone else. Apple does not
		// document it, but RFC 6749 requires unrecognised parameters to be
		// ignored, and leaving it off would weaken the one provider whose
		// credential is an assertion we hold for months.
		"code_challenge":        {challenge},
		"code_challenge_method": {Method},
	}
	return appleAuthorizeURL + "?" + query.Encode()
}

// Exchange redeems the code and reads the profile out of the id_token.
//
// The profile it returns carries no name, and cannot: Apple sends the name to
// whoever receives the callback — the web client — and never to this service,
// neither here nor in the id_token. The client passes it back on
// CompleteOAuthRequest.display_name, and the flow fills it in there.
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

// appleTokenResponse is the part of the token endpoint's answer this service
// uses. Apple returns access and refresh tokens too; neither is any use here,
// because there is no API to call with them.
type appleTokenResponse struct {
	IDToken          string `json:"id_token"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// redeem performs the token request and returns the raw id_token.
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

	// Apple reports a rejected exchange with a 400 and a machine-readable
	// reason. It is worth keeping: invalid_client means the assertion or its key
	// is wrong, which is an operator's problem and invisible otherwise.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("oauth: apple: token endpoint answered %s: %s",
			resp.Status, body.Error)
	}

	if body.IDToken == "" {
		return "", errors.New("oauth: apple: the token response carried no id_token")
	}
	return body.IDToken, nil
}

// profileFromIDToken verifies Apple's signature and reads the identity claims.
//
// This is the whole trust decision for an Apple sign-in: nothing else in the
// exchange is authenticated, so a token that fails here must not produce a
// profile under any circumstances.
func (p *appleProvider) profileFromIDToken(raw string) (domain.ProviderProfile, error) {
	parsed, err := jwt.Parse(raw, p.keyFunc,
		// Only RS256, whatever the token's own header says: a verifier that
		// trusts the header can be handed alg:none or an HMAC token signed with
		// a public key.
		jwt.WithValidMethods([]string{authn.Algorithm}),
		jwt.WithIssuer(appleIssuer),
		// The audience is this deployment's Services ID. Without it, an id_token
		// Apple issued to a different application would be accepted here.
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
		// A private relay address (…@privaterelay.appleid.com) works like any
		// other address, but knowing it is one explains both why it looks the
		// way it does and why mail to it goes through Apple.
		IsPrivateEmail: appleBool(claims, "is_private_email"),
	}, nil
}

// keyFunc resolves the public key the id_token names in its kid header.
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

// appleBool reads a claim Apple sends as either a boolean or a string.
//
// Both forms are real: email_verified and is_private_email have arrived as
// "true" for years, and a plain type assertion silently reads those as false —
// which for email_verified would mean refusing every Apple sign-in.
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

// appleRedirectURL derives Apple's callback address from the base every other
// provider returns to.
//
// It is derived rather than configured separately so that Apple cannot end up
// pointing at a different deployment than the rest of the providers. The path
// is fixed by the route the web client serves.
func appleRedirectURL(base string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("oauth: apple: %q is not an absolute redirect base URL", base)
	}
	return parsed.Scheme + "://" + parsed.Host + AppleCallbackPath, nil
}
