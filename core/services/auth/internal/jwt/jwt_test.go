package jwt_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/jwt"
)

const (
	testIssuer   = "https://auth.axon.test"
	testAudience = "axon"
	testTTL      = 15 * time.Minute
)

// rsaKey generates a key once per test binary: 2048-bit generation is slow
// enough that doing it per test would dominate the suite's runtime.
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

func issuerAndVerifier(t *testing.T, set *jwt.KeySet, now func() time.Time) (*jwt.Issuer, *jwt.Verifier) {
	t.Helper()

	iss, err := jwt.NewIssuer(jwt.IssuerConfig{
		Keys: set, Issuer: testIssuer, Audience: testAudience, TTL: testTTL, Now: now,
	})
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	ver, err := jwt.NewVerifier(jwt.VerifierConfig{
		Keys: set, Issuer: testIssuer, Audience: testAudience, Now: now,
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return iss, ver
}

func TestIssueThenVerify(t *testing.T) {
	set := keySet(t)
	iss, ver := issuerAndVerifier(t, set, nil)

	userID := uuid.New()

	raw, expiresAt, err := iss.Issue(userID, "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := ver.Verify(raw)
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
		t.Error("TokenID is empty; tokens are not individually identifiable")
	}
	if !claims.ExpiresAt.Equal(expiresAt.Truncate(time.Second)) {
		t.Errorf("ExpiresAt = %v, want %v", claims.ExpiresAt, expiresAt)
	}
}

// Two tokens for the same user must be distinguishable, otherwise nothing can
// be said about an individual session in the logs.
func TestEachTokenHasItsOwnID(t *testing.T) {
	iss, ver := issuerAndVerifier(t, keySet(t), nil)
	userID := uuid.New()

	first, _, err := iss.Issue(userID, "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	second, _, err := iss.Issue(userID, "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	a, err := ver.Verify(first)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	b, err := ver.Verify(second)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if a.TokenID == b.TokenID {
		t.Fatal("two tokens share a jti")
	}
}

func TestTokenNamesItsKey(t *testing.T) {
	iss, _ := issuerAndVerifier(t, keySet(t), nil)

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

func TestExpiredTokenIsReportedAsExpired(t *testing.T) {
	base := time.Now()
	clock := base

	set := keySet(t)
	iss, ver := issuerAndVerifier(t, set, func() time.Time { return clock })

	raw, _, err := iss.Issue(uuid.New(), "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Past the lifetime and past the leeway.
	clock = base.Add(testTTL + jwt.Leeway + time.Second)

	_, err = ver.Verify(raw)
	if !errors.Is(err, jwt.ErrExpired) {
		t.Fatalf("Verify error = %v, want ErrExpired", err)
	}
}

// Expiry is distinguishable from every other failure because the client acts on
// it differently: refresh, rather than sign in again.
func TestLeewayAbsorbsClockSkew(t *testing.T) {
	base := time.Now()
	clock := base

	iss, ver := issuerAndVerifier(t, keySet(t), func() time.Time { return clock })

	raw, _, err := iss.Issue(uuid.New(), "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Just past expiry but inside the skew allowance.
	clock = base.Add(testTTL + jwt.Leeway/2)

	if _, err := ver.Verify(raw); err != nil {
		t.Fatalf("a token within the leeway was rejected: %v", err)
	}
}

// The whole point of publishing several keys: a token signed by the key that is
// no longer active still verifies, which is what makes rotation possible without
// signing everyone out.
func TestTokenFromARetiredKeyStillVerifies(t *testing.T) {
	oldKey := jwt.PrivateKey{ID: "dev-1", Key: sharedKey}
	newKey := jwt.PrivateKey{ID: "dev-2", Key: sharedKey2}

	// Before rotation: dev-1 signs.
	before, err := jwt.NewKeySet(oldKey)
	if err != nil {
		t.Fatalf("NewKeySet: %v", err)
	}
	issBefore, _ := issuerAndVerifier(t, before, nil)

	raw, _, err := issBefore.Issue(uuid.New(), "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// After rotation: dev-2 signs, dev-1 is still published.
	after, err := jwt.NewKeySet(newKey, oldKey)
	if err != nil {
		t.Fatalf("NewKeySet: %v", err)
	}
	_, verAfter := issuerAndVerifier(t, after, nil)

	if _, err := verAfter.Verify(raw); err != nil {
		t.Fatalf("a token signed by the retired key was rejected after rotation: %v", err)
	}
}

func TestTokenFromAnUnpublishedKeyIsRejected(t *testing.T) {
	// Signed by a key the verifier has never published.
	foreign, err := jwt.NewKeySet(jwt.PrivateKey{ID: "attacker-1", Key: sharedKey2})
	if err != nil {
		t.Fatalf("NewKeySet: %v", err)
	}
	issForeign, _ := issuerAndVerifier(t, foreign, nil)

	raw, _, err := issForeign.Issue(uuid.New(), "eve@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	_, ver := issuerAndVerifier(t, keySet(t), nil)

	_, err = ver.Verify(raw)
	if !errors.Is(err, jwt.ErrUnknownKeyID) {
		t.Fatalf("Verify error = %v, want ErrUnknownKeyID", err)
	}
}

// alg confusion: a verifier that trusts the token's own header can be handed an
// HMAC token signed with the public key it publishes, or `alg: none`.
func TestAlgorithmConfusionIsRejected(t *testing.T) {
	_, ver := issuerAndVerifier(t, keySet(t), nil)

	claims := gojwt.MapClaims{
		"sub": uuid.NewString(),
		"iss": testIssuer,
		"aud": testAudience,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}

	t.Run("none", func(t *testing.T) {
		token := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims)
		token.Header["kid"] = "dev-1"

		raw, err := token.SignedString(gojwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}

		if _, err := ver.Verify(raw); err == nil {
			t.Fatal("a token with alg=none was accepted")
		}
	})

	t.Run("hmac signed with the published modulus", func(t *testing.T) {
		token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims)
		token.Header["kid"] = "dev-1"

		raw, err := token.SignedString(sharedKey.N.Bytes())
		if err != nil {
			t.Fatalf("sign: %v", err)
		}

		if _, err := ver.Verify(raw); err == nil {
			t.Fatal("an HMAC token signed with the public key was accepted")
		}
	})
}

func TestWrongIssuerOrAudienceIsRejected(t *testing.T) {
	set := keySet(t)
	iss, _ := issuerAndVerifier(t, set, nil)

	raw, _, err := iss.Issue(uuid.New(), "bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	tests := []struct {
		name     string
		issuer   string
		audience string
	}{
		{name: "wrong issuer", issuer: "https://evil.example", audience: testAudience},
		{name: "wrong audience", issuer: testIssuer, audience: "some-other-app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ver, err := jwt.NewVerifier(jwt.VerifierConfig{
				Keys: set, Issuer: tt.issuer, Audience: tt.audience,
			})
			if err != nil {
				t.Fatalf("NewVerifier: %v", err)
			}

			if _, err := ver.Verify(raw); !errors.Is(err, jwt.ErrInvalidToken) {
				t.Fatalf("Verify error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestGarbageTokensAreRejected(t *testing.T) {
	_, ver := issuerAndVerifier(t, keySet(t), nil)

	for _, raw := range []string{"", "not-a-token", "a.b.c", strings.Repeat("x", 500)} {
		if _, err := ver.Verify(raw); err == nil {
			t.Errorf("Verify(%.20q) accepted a malformed token", raw)
		}
	}
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
		// 65537 is AQAB, not AAEAAQ: the exponent is the shortest big-endian form.
		if k.E != "AQAB" {
			t.Errorf("key %q exponent = %q, want AQAB", k.Kid, k.E)
		}
		if k.N == "" {
			t.Errorf("key %q has an empty modulus", k.Kid)
		}
	}

	// The published modulus has to be the real one, or nobody can verify.
	nBytes, err := base64.RawURLEncoding.DecodeString(doc.Keys[0].N)
	if err != nil {
		t.Fatalf("modulus is not raw base64url: %v", err)
	}
	if new(big.Int).SetBytes(nBytes).Cmp(sharedKey.N) != 0 {
		t.Error("the published modulus does not match the signing key")
	}
}

// Nothing private may reach the JWKS document.
func TestJWKSCarriesNoPrivateMaterial(t *testing.T) {
	set := keySet(t)

	raw, err := set.JWKS()
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}

	for _, forbidden := range []string{"d", "p", "q", "dp", "dq", "qi"} {
		var doc struct {
			Keys []map[string]any `json:"keys"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		for _, k := range doc.Keys {
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

	// Both are what openssl produces, and neither is worth making an operator
	// care about.
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

func TestConfigurationIsValidatedUpFront(t *testing.T) {
	set := keySet(t)

	issuerCases := map[string]jwt.IssuerConfig{
		"no keys":     {Issuer: testIssuer, Audience: testAudience, TTL: testTTL},
		"no issuer":   {Keys: set, Audience: testAudience, TTL: testTTL},
		"no audience": {Keys: set, Issuer: testIssuer, TTL: testTTL},
		"zero ttl":    {Keys: set, Issuer: testIssuer, Audience: testAudience},
		"negative ttl": {
			Keys: set, Issuer: testIssuer, Audience: testAudience, TTL: -time.Minute,
		},
	}
	for name, cfg := range issuerCases {
		t.Run("issuer/"+name, func(t *testing.T) {
			if _, err := jwt.NewIssuer(cfg); err == nil {
				t.Fatal("an invalid issuer configuration was accepted")
			}
		})
	}

	verifierCases := map[string]jwt.VerifierConfig{
		"no keys":     {Issuer: testIssuer, Audience: testAudience},
		"no issuer":   {Keys: set, Audience: testAudience},
		"no audience": {Keys: set, Issuer: testIssuer},
	}
	for name, cfg := range verifierCases {
		t.Run("verifier/"+name, func(t *testing.T) {
			if _, err := jwt.NewVerifier(cfg); err == nil {
				t.Fatal("an invalid verifier configuration was accepted")
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
