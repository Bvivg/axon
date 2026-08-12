package authn

import (
	"crypto/rsa"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const Algorithm = "RS256"

const Leeway = 30 * time.Second

type Claims struct {
	UserID uuid.UUID

	TokenID uuid.UUID

	Email string

	IssuedAt  time.Time
	ExpiresAt time.Time
}

var (
	ErrExpired      = errors.New("authn: token expired")
	ErrInvalidToken = errors.New("authn: token invalid")
	ErrUnknownKeyID = errors.New("authn: token signed by an unknown key")
	ErrNoToken      = errors.New("authn: no token presented")
)

type KeySource interface {
	PublicKey(keyID string) (*rsa.PublicKey, bool)
}

type Config struct {
	Keys     KeySource
	Issuer   string
	Audience string

	Now func() time.Time
}

type Verifier struct {
	keys     KeySource
	issuer   string
	audience string
	now      func() time.Time
}

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

func (v *Verifier) Verify(raw string) (Claims, error) {
	if raw == "" {
		return Claims{}, ErrNoToken
	}

	parsed, err := jwt.Parse(raw, v.keyFunc,

		jwt.WithValidMethods([]string{Algorithm}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithLeeway(Leeway),
		jwt.WithTimeFunc(v.now),

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
