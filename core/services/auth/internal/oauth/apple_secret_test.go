package oauth

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
		"iss": testAppleTeamID,
		"sub": testAppleClientID,
	} {
		if got, _ := claims[claim].(string); got != want {
			t.Errorf("%s = %q, want %q", claim, got, want)
		}
	}

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

	if lifetime := expiry.Sub(now); lifetime > appleMaxClientSecretTTL {
		t.Errorf("the assertion lives %s, which is beyond Apple's limit of %s",
			lifetime, appleMaxClientSecretTTL)
	}
}

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

	now = now.Add(appleClientSecretTTL - appleClientSecretRenewBefore - time.Minute)
	again, err := secret.value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	if again != first {
		t.Error("a fresh assertion was signed while the cached one was still good")
	}

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

func TestAppleRefusesAKeyItCannotUse(t *testing.T) {
	valid, _ := appleTestConfig(t)

	rsaKey := rsaPEM(t)

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
