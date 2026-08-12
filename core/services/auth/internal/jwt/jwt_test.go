package jwt_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/auth/internal/jwt"
)

const (
	testIssuer   = "https://auth.axon.test"
	testAudience = "axon"
	testTTL      = 15 * time.Minute
)

var (
	sharedKey  *rsa.PrivateKey
	sharedKey2 *rsa.PrivateKey
)

func init() {
	var err error
	if sharedKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		panic(err)
	}
	if sharedKey2, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		panic(err)
	}
}

func keySet(t *testing.T, additional ...jwt.PrivateKey) *jwt.KeySet {
	t.Helper()

	set, err := jwt.NewKeySet(jwt.PrivateKey{ID: "dev-1", Key: sharedKey}, additional...)
	if err != nil {
		t.Fatalf("NewKeySet: %v", err)
	}
	return set
}

func issuer(t *testing.T, set *jwt.KeySet) *jwt.Issuer {
	t.Helper()

	iss, err := jwt.NewIssuer(jwt.IssuerConfig{
		Keys: set, Issuer: testIssuer, Audience: testAudience, TTL: testTTL,
	})
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	return iss
}

func verifier(t *testing.T, keys authn.KeySource) *authn.Verifier {
	t.Helper()

	v, err := authn.NewVerifier(authn.Config{
		Keys: keys, Issuer: testIssuer, Audience: testAudience,
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func TestIssuedTokensVerify(t *testing.T) {
	set := keySet(t)
	iss := issuer(t, set)
	v := verifier(t, set)

	userID := uuid.New()

	raw, expiresAt, err := iss.Issue(userID, "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := v.Verify(raw)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("UserID = %v, want %v", claims.UserID, userID)
	}
	if claims.Email != "bob@example.com" {
		t.Errorf("Email = %q, want bob@example.com", claims.Email)
	}
	if claims.TokenID == uuid.Nil {
		t.Error("TokenID is empty; sessions are not individually identifiable")
	}
	if !claims.ExpiresAt.Equal(expiresAt.Truncate(time.Second)) {
		t.Errorf("ExpiresAt = %v, want %v", claims.ExpiresAt, expiresAt)
	}
}

func TestEachTokenHasItsOwnID(t *testing.T) {
	set := keySet(t)
	iss := issuer(t, set)
	v := verifier(t, set)

	userID := uuid.New()

	first, _, err := iss.Issue(userID, "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	second, _, err := iss.Issue(userID, "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	a, err := v.Verify(first)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	b, err := v.Verify(second)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if a.TokenID == b.TokenID {
		t.Fatal("two tokens share a jti")
	}
}

func TestTokenNamesItsKey(t *testing.T) {
	iss := issuer(t, keySet(t))

	raw, _, err := iss.Issue(uuid.New(), "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	parsed, _, err := gojwt.NewParser().ParseUnverified(raw, gojwt.MapClaims{})
	if err != nil {
		t.Fatalf("ParseUnverified: %v", err)
	}

	if got := parsed.Header["kid"]; got != "dev-1" {
		t.Errorf("kid header = %v, want dev-1", got)
	}
	if got := parsed.Header["alg"]; got != jwt.Algorithm {
		t.Errorf("alg header = %v, want %s", got, jwt.Algorithm)
	}
}

func TestTokenFromARetiredKeyStillVerifies(t *testing.T) {
	oldKey := jwt.PrivateKey{ID: "dev-1", Key: sharedKey}
	newKey := jwt.PrivateKey{ID: "dev-2", Key: sharedKey2}

	before, err := jwt.NewKeySet(oldKey)
	if err != nil {
		t.Fatalf("NewKeySet: %v", err)
	}

	raw, _, err := issuer(t, before).Issue(uuid.New(), "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	after, err := jwt.NewKeySet(newKey, oldKey)
	if err != nil {
		t.Fatalf("NewKeySet: %v", err)
	}

	if _, err := verifier(t, after).Verify(raw); err != nil {
		t.Fatalf("a token signed by the retired key was rejected after rotation: %v", err)
	}
}

func TestJWKSRoundTripsThroughTheSharedParser(t *testing.T) {
	set := keySet(t, jwt.PrivateKey{ID: "dev-2", Key: sharedKey2})

	document, err := set.JWKS()
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}

	parsed, err := authn.ParseJWKS(document)
	if err != nil {
		t.Fatalf("the shared parser rejected our own JWKS document: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("parsed %d keys, want 2", len(parsed))
	}

	raw, _, err := issuer(t, set).Issue(uuid.New(), "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := verifier(t, staticKeys(parsed)).Verify(raw); err != nil {
		t.Fatalf("a token did not verify against the published key set: %v", err)
	}
}

type staticKeys map[string]*rsa.PublicKey

func (s staticKeys) PublicKey(keyID string) (*rsa.PublicKey, bool) {
	k, ok := s[keyID]
	return k, ok
}

func TestJWKSDocument(t *testing.T) {
	set := keySet(t, jwt.PrivateKey{ID: "dev-2", Key: sharedKey2})

	raw, err := set.JWKS()
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}

	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the JWKS document is not valid JSON: %v", err)
	}

	if len(doc.Keys) != 2 {
		t.Fatalf("got %d keys, want 2 — rotation needs the old key published alongside the new one", len(doc.Keys))
	}

	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Use != "sig" || k.Alg != jwt.Algorithm {
			t.Errorf("key %q has kty=%q use=%q alg=%q", k.Kid, k.Kty, k.Use, k.Alg)
		}

		if k.E != "AQAB" {
			t.Errorf("key %q exponent = %q, want AQAB", k.Kid, k.E)
		}
	}

	nBytes, err := base64.RawURLEncoding.DecodeString(doc.Keys[0].N)
	if err != nil {
		t.Fatalf("modulus is not raw base64url: %v", err)
	}
	if new(big.Int).SetBytes(nBytes).Cmp(sharedKey.N) != 0 {
		t.Error("the published modulus does not match the signing key")
	}
}

func TestJWKSCarriesNoPrivateMaterial(t *testing.T) {
	raw, err := keySet(t).JWKS()
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}

	var doc struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, k := range doc.Keys {
		for _, forbidden := range []string{"d", "p", "q", "dp", "dq", "qi"} {
			if _, present := k[forbidden]; present {
				t.Errorf("the JWKS document contains the private field %q", forbidden)
			}
		}
	}
}

func TestParsePrivateKeyPEM(t *testing.T) {
	pkcs1 := pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(sharedKey),
	})

	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(sharedKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes})

	for name, data := range map[string][]byte{"pkcs1": pkcs1, "pkcs8": pkcs8} {
		t.Run(name, func(t *testing.T) {
			key, err := jwt.ParsePrivateKeyPEM(data)
			if err != nil {
				t.Fatalf("ParsePrivateKeyPEM: %v", err)
			}
			if key.N.Cmp(sharedKey.N) != 0 {
				t.Error("parsed a different key than the one encoded")
			}
		})
	}

	t.Run("not pem", func(t *testing.T) {
		if _, err := jwt.ParsePrivateKeyPEM([]byte("hello")); err == nil {
			t.Fatal("non-PEM input was accepted")
		}
	})
}

func TestShortKeysAreRefused(t *testing.T) {
	short, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	if _, err := jwt.NewKeySet(jwt.PrivateKey{ID: "weak", Key: short}); err == nil {
		t.Fatal("a 1024-bit signing key was accepted")
	}

	encoded := pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(short),
	})
	if _, err := jwt.ParsePrivateKeyPEM(encoded); err == nil {
		t.Fatal("a 1024-bit key was accepted from PEM")
	}
}

func TestIssuerConfigurationIsValidatedUpFront(t *testing.T) {
	set := keySet(t)

	cases := map[string]jwt.IssuerConfig{
		"no keys":      {Issuer: testIssuer, Audience: testAudience, TTL: testTTL},
		"no issuer":    {Keys: set, Audience: testAudience, TTL: testTTL},
		"no audience":  {Keys: set, Issuer: testIssuer, TTL: testTTL},
		"zero ttl":     {Keys: set, Issuer: testIssuer, Audience: testAudience},
		"negative ttl": {Keys: set, Issuer: testIssuer, Audience: testAudience, TTL: -time.Minute},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := jwt.NewIssuer(cfg); err == nil {
				t.Fatal("an invalid issuer configuration was accepted")
			}
		})
	}
}

func TestKeySetRejectsIncompleteKeys(t *testing.T) {
	valid := jwt.PrivateKey{ID: "dev-1", Key: sharedKey}

	cases := map[string][]jwt.PrivateKey{
		"active without id":     {{Key: sharedKey}},
		"active without key":    {{ID: "dev-1"}},
		"additional without id": {valid, {Key: sharedKey2}},
	}

	for name, keys := range cases {
		t.Run(name, func(t *testing.T) {
			var err error
			if len(keys) == 1 {
				_, err = jwt.NewKeySet(keys[0])
			} else {
				_, err = jwt.NewKeySet(keys[0], keys[1:]...)
			}
			if err == nil {
				t.Fatal("an incomplete key set was accepted")
			}
		})
	}
}

func TestKeyIDsAreStable(t *testing.T) {
	set := keySet(t, jwt.PrivateKey{ID: "dev-2", Key: sharedKey2})

	first := set.KeyIDs()
	for range 10 {
		got := set.KeyIDs()
		if len(got) != len(first) {
			t.Fatalf("KeyIDs() length changed: %v vs %v", got, first)
		}
		for i := range got {
			if got[i] != first[i] {
				t.Fatalf("KeyIDs() order is not stable: %v vs %v", got, first)
			}
		}
	}
}
