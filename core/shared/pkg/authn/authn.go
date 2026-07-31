// Package authn verifies the access tokens every Axon service trusts.
//
// It lives in shared/ because verification has to be identical everywhere. Two
// services that disagree about what a valid token is are a security bug waiting
// for the right input, and the disagreement would only show up as a request that
// one service accepts and another does not.
//
// Only verification is here. Issuing lives in the auth service, which is the
// only thing holding a private key.
package authn

import (
	"crypto/rsa"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Algorithm is the only signing algorithm accepted anywhere in the system.
//
// Pinning it is not a preference, it is the fix for the alg-confusion attack: a
// verifier that trusts the token's own header can be handed `alg: none`, or an
// HMAC token signed with the public key the service itself publishes.
const Algorithm = "RS256"

// Leeway absorbs clock skew between services when checking exp and iat.
const Leeway = 30 * time.Second

// Claims is the verified payload of an access token.
type Claims struct {
	// UserID is the subject: the account the request acts as.
	UserID uuid.UUID
	// TokenID is the jti, unique per token. It makes an individual session
	// identifiable in logs across services.
	TokenID uuid.UUID
	// Email is carried for logging and display. It is never authorization
	// input — the subject is what identifies the account.
	Email string

	IssuedAt  time.Time
	ExpiresAt time.Time
}

// Failures a verifier can report. Expiry is separate because a client acts on it
// differently: refresh, rather than sign in again. Everything else collapses,
// because which part of a forged token failed is not something an attacker gets
// to learn.
var (
	ErrExpired      = errors.New("authn: token expired")
	ErrInvalidToken = errors.New("authn: token invalid")
	ErrUnknownKeyID = errors.New("authn: token signed by an unknown key")
	ErrNoToken      = errors.New("authn: no token presented")
)

// KeySource resolves the public key a token names in its kid header.
//
// The auth service satisfies it from the keys it holds; every other service
// satisfies it from the JWKS document it fetched. Same verifier either way.
type KeySource interface {
	// PublicKey returns the key published under keyID. The bool reports whether
	// it is known, so a caller can tell "no such key" from a transport failure.
	PublicKey(keyID string) (*rsa.PublicKey, bool)
}

// Config configures a Verifier.
type Config struct {
	Keys     KeySource
	Issuer   string
	Audience string

	// Now overrides the clock. Tests set it; production leaves it nil.
	Now func() time.Time
}

// Verifier checks access tokens.
type Verifier struct {
	keys     KeySource
	issuer   string
	audience string
	now      func() time.Time
}

// NewVerifier validates the configuration and returns a Verifier.
func NewVerifier(cfg Config) (*Verifier, error) {
	switch {
	case cfg.Keys == nil:
		return nil, errors.New("authn: verifier needs a key source")
	case cfg.Issuer == "":
		return nil, errors.New("authn: verifier needs an issuer name")
	case cfg.Audience == "":
		return nil, errors.New("authn: verifier needs an audience")
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Verifier{keys: cfg.Keys, issuer: cfg.Issuer, audience: cfg.Audience, now: now}, nil
}

// Verify parses and checks a token, returning its claims.
func (v *Verifier) Verify(raw string) (Claims, error) {
	if raw == "" {
		return Claims{}, ErrNoToken
	}

	parsed, err := jwt.Parse(raw, v.keyFunc,
		// Only RS256, whatever the token's header claims.
		jwt.WithValidMethods([]string{Algorithm}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithLeeway(Leeway),
		jwt.WithTimeFunc(v.now),
		// A token without an expiry would be valid forever.
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return Claims{}, ErrExpired
		case errors.Is(err, ErrUnknownKeyID):
			return Claims{}, ErrUnknownKeyID
		default:
			return Claims{}, ErrInvalidToken
		}
	}

	return v.claims(parsed)
}

// keyFunc resolves the public key named by the token's kid header.
func (v *Verifier) keyFunc(token *jwt.Token) (any, error) {
	keyID, ok := token.Header["kid"].(string)
	if !ok || keyID == "" {
		return nil, ErrInvalidToken
	}

	key, ok := v.keys.PublicKey(keyID)
	if !ok {
		return nil, ErrUnknownKeyID
	}
	return key, nil
}

// claims converts a verified token into the typed form.
func (v *Verifier) claims(token *jwt.Token) (Claims, error) {
	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return Claims{}, ErrInvalidToken
	}

	subject, err := mapClaims.GetSubject()
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	userID, err := uuid.Parse(subject)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}

	expiresAt, err := mapClaims.GetExpirationTime()
	if err != nil || expiresAt == nil {
		return Claims{}, ErrInvalidToken
	}

	issuedAt, err := mapClaims.GetIssuedAt()
	if err != nil {
		return Claims{}, ErrInvalidToken
	}

	// A missing jti is not worth rejecting a token over: it is used for
	// correlation, not for the decision to trust the token.
	tokenID, _ := uuid.Parse(stringClaim(mapClaims, "jti"))

	claims := Claims{
		UserID:    userID,
		TokenID:   tokenID,
		Email:     stringClaim(mapClaims, "email"),
		ExpiresAt: expiresAt.Time,
	}
	if issuedAt != nil {
		claims.IssuedAt = issuedAt.Time
	}

	return claims, nil
}

func stringClaim(claims jwt.MapClaims, key string) string {
	s, _ := claims[key].(string)
	return s
}
