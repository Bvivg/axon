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

// Apple's client secret is not a string an operator copies out of a console: it
// is a short-lived ES256 assertion this service signs with a downloaded .p8 key
// and has to keep re-minting. The constants below are the whole policy.
const (
	// appleClientSecretTTL is how long a minted secret claims to be valid.
	//
	// Apple's ceiling is six months, and using it would mean one long-lived
	// bearer credential sitting in memory for half a year. Thirty minutes costs
	// one signature every half hour and bounds what a leaked assertion is worth.
	appleClientSecretTTL = 30 * time.Minute

	// appleClientSecretRenewBefore is how early the cached secret is replaced,
	// so an exchange never starts with an assertion that expires mid-flight.
	appleClientSecretRenewBefore = 5 * time.Minute

	// appleMaxClientSecretTTL is Apple's own limit: 15777000 seconds, roughly
	// six months. It is checked at construction rather than trusted, so a future
	// edit to the TTL above fails at startup instead of at somebody's sign-in.
	appleMaxClientSecretTTL = 15777000 * time.Second

	// appleAudience is what Apple expects in the assertion's aud claim.
	appleAudience = appleIssuer
)

// appleClientSecret mints and caches the assertion Apple accepts in place of a
// client secret.
//
// It is a type of its own rather than a function because of the cache: minting
// per request would sign an ECDSA assertion inside every sign-in for no reason,
// and holding one forever would defeat the point of a short expiry.
type appleClientSecret struct {
	teamID   string
	clientID string
	keyID    string
	key      *ecdsa.PrivateKey

	ttl         time.Duration
	renewBefore time.Duration
	now         func() time.Time

	// mu guards the cached assertion. Sign-ins are concurrent, and two of them
	// arriving at a stale secret must produce one new assertion, not two.
	mu        sync.Mutex
	cached    string
	expiresAt time.Time
}

// newAppleClientSecret validates the credentials and prepares the signer.
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

// value returns a usable assertion, minting a new one when the cached one is
// gone or close enough to expiry that Apple might see it as expired.
func (s *appleClientSecret) value() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if s.cached != "" && now.Before(s.expiresAt.Add(-s.renewBefore)) {
		return s.cached, nil
	}

	expiresAt := now.Add(s.ttl)

	// Claims per Apple's "Generate and validate tokens": the team owns the key,
	// so it is the issuer; the Services ID the secret authenticates is the
	// subject; and the audience is Apple itself, which is what stops an
	// assertion from being replayed against anyone else.
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": s.teamID,
		"sub": s.clientID,
		"aud": appleAudience,
		"iat": now.Unix(),
		"exp": expiresAt.Unix(),
	})
	// The key id names which of the team's keys signed this, and Apple has no
	// other way to look it up.
	token.Header["kid"] = s.keyID

	signed, err := token.SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("oauth: apple: sign client secret: %w", err)
	}

	s.cached, s.expiresAt = signed, expiresAt
	return signed, nil
}

// parseApplePrivateKey reads the .p8 file Apple issues.
//
// It is a PKCS#8-wrapped P-256 key. The curve is checked rather than assumed:
// ES256 is defined only over P-256, and a key on another curve would produce
// signatures Apple rejects with an error that says nothing about the cause.
func parseApplePrivateKey(data []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("oauth: apple: no PEM block found in the private key")
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// The error can quote key bytes, so it is not wrapped.
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
