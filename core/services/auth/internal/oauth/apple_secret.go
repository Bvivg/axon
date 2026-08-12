package oauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	appleClientSecretTTL = 30 * time.Minute

	appleClientSecretRenewBefore = 5 * time.Minute

	appleMaxClientSecretTTL = 15777000 * time.Second

	appleAudience = appleIssuer
)

type appleClientSecret struct {
	teamID   string
	clientID string
	keyID    string
	key      *ecdsa.PrivateKey

	ttl         time.Duration
	renewBefore time.Duration
	now         func() time.Time

	mu        sync.Mutex
	cached    string
	expiresAt time.Time
}

func newAppleClientSecret(cfg AppleConfig) (*appleClientSecret, error) {
	switch {
	case strings.TrimSpace(cfg.TeamID) == "":
		return nil, errors.New("oauth: apple: team id is required")
	case strings.TrimSpace(cfg.ClientID) == "":
		return nil, errors.New("oauth: apple: client id is required")
	case strings.TrimSpace(cfg.KeyID) == "":
		return nil, errors.New("oauth: apple: key id is required")
	}

	key, err := parseApplePrivateKey([]byte(cfg.PrivateKey))
	if err != nil {
		return nil, err
	}

	if appleClientSecretTTL > appleMaxClientSecretTTL {
		return nil, fmt.Errorf("oauth: apple: client secret lifetime %s exceeds apple's limit of %s",
			appleClientSecretTTL, appleMaxClientSecretTTL)
	}

	return &appleClientSecret{
		teamID:      strings.TrimSpace(cfg.TeamID),
		clientID:    strings.TrimSpace(cfg.ClientID),
		keyID:       strings.TrimSpace(cfg.KeyID),
		key:         key,
		ttl:         appleClientSecretTTL,
		renewBefore: appleClientSecretRenewBefore,
		now:         time.Now,
	}, nil
}

func (s *appleClientSecret) value() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if s.cached != "" && now.Before(s.expiresAt.Add(-s.renewBefore)) {
		return s.cached, nil
	}

	expiresAt := now.Add(s.ttl)

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": s.teamID,
		"sub": s.clientID,
		"aud": appleAudience,
		"iat": now.Unix(),
		"exp": expiresAt.Unix(),
	})

	token.Header["kid"] = s.keyID

	signed, err := token.SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("oauth: apple: sign client secret: %w", err)
	}

	s.cached, s.expiresAt = signed, expiresAt
	return signed, nil
}

func parseApplePrivateKey(data []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("oauth: apple: no PEM block found in the private key")
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {

		return nil, errors.New("oauth: apple: the private key is not a PKCS#8 key")
	}

	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("oauth: apple: the private key is %T, want an ECDSA key", parsed)
	}

	if key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("oauth: apple: the private key is on curve %s, want P-256",
			key.Curve.Params().Name)
	}

	return key, nil
}
