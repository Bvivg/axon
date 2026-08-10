package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"
)

// Issuer mints access tokens.
//
// Only issuing lives here, because only this service holds a private key.
// Verification is shared/pkg/authn, used unchanged by every service including
// this one: two verifiers that disagree about what a valid token is would be a
// security bug waiting for the right input.
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
	// check against without trying each one — and so a rotation works at all.
	active := i.keys.Active()
	token.Header["kid"] = active.ID

	signed, err := token.SignedString(active.Key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("jwt: sign token: %w", err)
	}

	return signed, expiresAt, nil
}

// Algorithm is re-exported so callers do not have to import authn just to name
// the signing algorithm in a log line or a test.
const Algorithm = authn.Algorithm
