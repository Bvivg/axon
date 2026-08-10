package oauth

// In the package rather than beside it: Apple's endpoints are package variables
// so a test can point them at a stand-in, and the clock is a struct field for
// the same reason.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testAppleClientID = "com.example.axon.service"
	testAppleTeamID   = "TEAM123456"
	testAppleKeyID    = "KEY7890123"
)

// appleTestKey generates the kind of key Apple issues: a P-256 key in a PKCS#8
// PEM block, which is what a .p8 file contains.
//
// Generated per test rather than checked in, because a private key in the
// repository is a private key in the repository however loudly the comment above
// it says otherwise.
func appleTestKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-256 key: %v", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	encoded := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	return key, string(encoded)
}

// appleTestConfig is a fully configured Apple, with a freshly generated key.
func appleTestConfig(t *testing.T) (AppleConfig, *ecdsa.PrivateKey) {
	t.Helper()

	key, encoded := appleTestKey(t)
	return AppleConfig{
		ClientID:   testAppleClientID,
		TeamID:     testAppleTeamID,
		KeyID:      testAppleKeyID,
		PrivateKey: encoded,
	}, key
}

// The client secret is not a string from a console: it is an assertion this
// service signs. Apple checks every one of these claims, and getting any of them
// wrong fails the exchange with "invalid_client" and nothing else to go on.
func TestAppleClientSecretCarriesTheClaimsAppleChecks(t *testing.T) {
	cfg, key := appleTestConfig(t)

	secret, err := newAppleClientSecret(cfg)
	if err != nil {
		t.Fatalf("newAppleClientSecret: %v", err)
	}

	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	secret.now = func() time.Time { return now }

	assertion, err := secret.value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}

	// Parsed with the public half of the key that signed it, so this asserts the
	// signature as well as the claims. ES256 only: an assertion this service will
	// accept back under another algorithm is a verification bug waiting to be
	// copied somewhere it matters.
	parsed, err := jwt.Parse(assertion, func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
		jwt.WithValidMethods([]string{"ES256"}),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("the assertion does not verify against its own key: %v", err)
	}

	if got := parsed.Header["kid"]; got != testAppleKeyID {
		t.Errorf("kid = %v, want the key id — Apple cannot find the key without it", got)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims are %T", parsed.Claims)
	}

	for claim, want := range map[string]string{
		"iss": testAppleTeamID,   // the team owns the key
		"sub": testAppleClientID, // the Services ID it authenticates
	} {
		if got, _ := claims[claim].(string); got != want {
			t.Errorf("%s = %q, want %q", claim, got, want)
		}
	}

	// The audience is Apple itself: it is what stops the assertion being replayed
	// against anything else that might accept it.
	audience, err := claims.GetAudience()
	if err != nil || len(audience) != 1 || audience[0] != appleIssuer {
		t.Errorf("aud = %v, want [%s]", audience, appleIssuer)
	}

	expiry, err := claims.GetExpirationTime()
	if err != nil || expiry == nil {
		t.Fatalf("exp is missing: %v", err)
	}
	if !expiry.After(now) {
		t.Errorf("exp = %s, want a time after %s", expiry, now)
	}
	// Apple refuses anything longer than six months, and a secret that long
	// defeats the point of signing a fresh one.
	if lifetime := expiry.Sub(now); lifetime > appleMaxClientSecretTTL {
		t.Errorf("the assertion lives %s, which is beyond Apple's limit of %s",
			lifetime, appleMaxClientSecretTTL)
	}
}

// Minting per exchange would sign an ECDSA assertion inside every sign-in, and
// holding one forever would make the short expiry meaningless. The cache has to
// do both: reuse, then rotate.
func TestAppleClientSecretIsReusedUntilItNearsExpiry(t *testing.T) {
	cfg, _ := appleTestConfig(t)

	secret, err := newAppleClientSecret(cfg)
	if err != nil {
		t.Fatalf("newAppleClientSecret: %v", err)
	}

	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	secret.now = func() time.Time { return now }

	first, err := secret.value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}

	// Still comfortably inside the window: the same assertion, not a new one.
	now = now.Add(appleClientSecretTTL - appleClientSecretRenewBefore - time.Minute)
	again, err := secret.value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	if again != first {
		t.Error("a fresh assertion was signed while the cached one was still good")
	}

	// Inside the renewal window, and Apple would still accept the old one — which
	// is the point: it is replaced before it can expire mid-exchange.
	now = now.Add(2 * time.Minute)
	rotated, err := secret.value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	if rotated == first {
		t.Fatal("the assertion was not rotated as it approached expiry")
	}

	if expiryOf(t, rotated).Before(expiryOf(t, first)) {
		t.Error("the rotated assertion expires earlier than the one it replaced")
	}
}

// A .p8 that is not what it claims to be has to fail at construction, where an
// operator is looking, rather than on somebody's sign-in.
func TestAppleRefusesAKeyItCannotUse(t *testing.T) {
	valid, _ := appleTestConfig(t)

	// A PKCS#8 block holding an RSA key: a real file of the wrong kind. It is the
	// mistake of reaching for the JWT signing key instead of the .p8.
	rsaKey := rsaPEM(t)
	// A PEM block whose contents are not a key at all: a truncated or mangled
	// copy-paste, which is the other way this goes wrong.
	garbage := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not a key")}))

	for name, cfg := range map[string]AppleConfig{
		"no key": {ClientID: valid.ClientID, TeamID: valid.TeamID, KeyID: valid.KeyID},
		"not a PEM block": {
			ClientID: valid.ClientID, TeamID: valid.TeamID, KeyID: valid.KeyID,
			PrivateKey: "-- definitely not a key --",
		},
		"a PEM block that is not a key": {
			ClientID: valid.ClientID, TeamID: valid.TeamID, KeyID: valid.KeyID,
			PrivateKey: garbage,
		},
		"an RSA key rather than an EC one": {
			ClientID: valid.ClientID, TeamID: valid.TeamID, KeyID: valid.KeyID,
			PrivateKey: rsaKey,
		},
		"no team id": {ClientID: valid.ClientID, KeyID: valid.KeyID, PrivateKey: valid.PrivateKey},
		"no key id":  {ClientID: valid.ClientID, TeamID: valid.TeamID, PrivateKey: valid.PrivateKey},
		"no client id": {
			TeamID: valid.TeamID, KeyID: valid.KeyID, PrivateKey: valid.PrivateKey,
		},
	} {
		if _, err := newAppleClientSecret(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// ES256 is defined only over P-256. A key on another curve produces signatures
// Apple rejects with a message that says nothing about the cause.
func TestAppleRefusesAKeyOnTheWrongCurve(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-384 key: %v", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	_, err = parseApplePrivateKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	if err == nil {
		t.Fatal("a P-384 key was accepted")
	}
	if !strings.Contains(err.Error(), "P-256") {
		t.Errorf("the error does not say what is wrong: %v", err)
	}
}

// expiryOf reads exp without verifying: the signature is asserted elsewhere.
func expiryOf(t *testing.T, assertion string) time.Time {
	t.Helper()

	var claims jwt.MapClaims
	if _, _, err := jwt.NewParser().ParseUnverified(assertion, &claims); err != nil {
		t.Fatalf("parse assertion: %v", err)
	}

	expiry, err := claims.GetExpirationTime()
	if err != nil || expiry == nil {
		t.Fatalf("assertion has no exp: %v", err)
	}
	return expiry.Time
}
