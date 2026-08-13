package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"
)

type Issuer struct {
	keys     *KeySet
	issuer   string
	audience string
	ttl      time.Duration
	now      func() time.Time
}

type IssuerConfig struct {
	Keys     *KeySet
	Issuer   string
	Audience string
	TTL      time.Duration

	Now func() time.Time
}

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

func (i *Issuer) TTL() time.Duration { return i.ttl }

func (i *Issuer) Issue(userID uuid.UUID, email string, familyID uuid.UUID) (string, time.Time, error) {
	now := i.now().UTC()
	expiresAt := now.Add(i.ttl)

	claims := jwt.MapClaims{
		"sub":   userID.String(),
		"jti":   uuid.NewString(),
		"fid":   familyID.String(),
		"iss":   i.issuer,
		"aud":   i.audience,
		"iat":   now.Unix(),
		"exp":   expiresAt.Unix(),
		"email": email,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

	active := i.keys.Active()
	token.Header["kid"] = active.ID

	signed, err := token.SignedString(active.Key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("jwt: sign token: %w", err)
	}

	return signed, expiresAt, nil
}

const Algorithm = authn.Algorithm
