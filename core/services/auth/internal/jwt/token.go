package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Algorithm is the only signing algorithm accepted anywhere in the system.
//
// Pinning it is not a preference, it is the fix for the alg-confusion attack:
// a verifier that trusts the token's own header can be handed `alg: none`, or
// an HMAC token signed with the public key it published.
const Algorithm = "RS256"

// Leeway absorbs clock skew between services when checking exp and iat.
const Leeway = 30 * time.Second

// Claims is the payload of an access token.
type Claims struct {
	// UserID is the subject.
	UserID uuid.UUID
	// TokenID is the jti. Unique per token, which is what makes an individual
	// token identifiable in logs and revocable later if a deny list is added.
	TokenID   uuid.UUID
	Issuer    string
	Audience  string
	IssuedAt  time.Time
	ExpiresAt time.Time
	// Email travels in the token so downstream services can log and display it
	// without a lookup. Never treat it as authorization input — the subject is
	// what identifies the account.
	Email string
}

// Errors a verifier can return. They are distinct because the caller reacts
// differently to each: an expired token means refresh, everything else means
// sign in again.
var (
	ErrExpired      = errors.New("jwt: token expired")
	ErrInvalidToken = errors.New("jwt: token invalid")
	ErrUnknownKeyID = errors.New("jwt: token signed by an unknown key")
)

// Issuer mints access tokens.
type Issuer struct {
	keys     *KeySet
	issuer   string
	audience string
	ttl      time.Duration
	now      func() time.Time
}

// IssuerConfig configures an Issuer.
type IssuerConfig struct {
	Keys     *KeySet
	Issuer   string
	Audience string
	TTL      time.Duration

	// Now overrides the clock. Tests set it; production leaves it nil.
	Now func() time.Time
}

// NewIssuer validates the configuration and returns an Issuer.
func NewIssuer(cfg IssuerConfig) (*Issuer, error) {
	switch {
	case cfg.Keys == nil:
		return nil, errors.New("jwt: issuer needs a key set")
	case cfg.Issuer == "":
		return nil, errors.New("jwt: issuer needs an issuer name")
	case cfg.Audience == "":
		return nil, errors.New("jwt: issuer needs an audience")
	case cfg.TTL <= 0:
		return nil, errors.New("jwt: issuer needs a positive token lifetime")
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Issuer{
		keys:     cfg.Keys,
		issuer:   cfg.Issuer,
		audience: cfg.Audience,
		ttl:      cfg.TTL,
		now:      now,
	}, nil
}

// TTL returns the lifetime issued tokens get.
func (i *Issuer) TTL() time.Duration { return i.ttl }

// Issue signs an access token for the user and returns it with its expiry.
func (i *Issuer) Issue(userID uuid.UUID, email string) (string, time.Time, error) {
	now := i.now().UTC()
	expiresAt := now.Add(i.ttl)

	claims := jwt.MapClaims{
		"sub":   userID.String(),
		"jti":   uuid.NewString(),
		"iss":   i.issuer,
		"aud":   i.audience,
		"iat":   now.Unix(),
		"exp":   expiresAt.Unix(),
		"email": email,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

	// The key id goes in the header so a verifier knows which published key to
	// check against without trying each one.
	active := i.keys.Active()
	token.Header["kid"] = active.ID

	signed, err := token.SignedString(active.Key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("jwt: sign token: %w", err)
	}

	return signed, expiresAt, nil
}

// Verifier checks access tokens against a key set.
//
// It lives here so auth and the gateway share one implementation: two verifiers
// that disagree about what a valid token is would be a security bug waiting for
// the right input.
type Verifier struct {
	keys     *KeySet
	issuer   string
	audience string
	now      func() time.Time
}

// VerifierConfig configures a Verifier.
type VerifierConfig struct {
	Keys     *KeySet
	Issuer   string
	Audience string
	Now      func() time.Time
}

// NewVerifier validates the configuration and returns a Verifier.
func NewVerifier(cfg VerifierConfig) (*Verifier, error) {
	switch {
	case cfg.Keys == nil:
		return nil, errors.New("jwt: verifier needs a key set")
	case cfg.Issuer == "":
		return nil, errors.New("jwt: verifier needs an issuer name")
	case cfg.Audience == "":
		return nil, errors.New("jwt: verifier needs an audience")
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Verifier{keys: cfg.Keys, issuer: cfg.Issuer, audience: cfg.Audience, now: now}, nil
}

// Verify parses and checks a token, returning its claims.
func (v *Verifier) Verify(raw string) (Claims, error) {
	parsed, err := jwt.Parse(raw, v.keyFunc,
		// Only RS256 is accepted, whatever the token's header says.
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
			// The underlying reason is not returned to the caller: which part of
			// a forged token failed is not something an attacker should learn.
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
		return nil, fmt.Errorf("%w: %q", ErrUnknownKeyID, keyID)
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

	// jti is expected but a missing one is not worth rejecting a token over: it
	// is used for correlation, not for the decision to trust the token.
	tokenID, _ := uuid.Parse(stringClaim(mapClaims, "jti"))

	issuedAt, err := mapClaims.GetIssuedAt()
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	expiresAt, err := mapClaims.GetExpirationTime()
	if err != nil || expiresAt == nil {
		return Claims{}, ErrInvalidToken
	}

	claims := Claims{
		UserID:    userID,
		TokenID:   tokenID,
		Issuer:    v.issuer,
		Audience:  v.audience,
		ExpiresAt: expiresAt.Time,
		Email:     stringClaim(mapClaims, "email"),
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
