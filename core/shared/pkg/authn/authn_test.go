package authn_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

const (
	testIssuer   = "https://auth.axon.test"
	testAudience = "axon"
)

var (
	key1 *rsa.PrivateKey
	key2 *rsa.PrivateKey
)

func init() {
	var err error
	if key1, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		panic(err)
	}
	if key2, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		panic(err)
	}
}

// staticKeys is a KeySource over a fixed map.
type staticKeys map[string]*rsa.PublicKey

func (s staticKeys) PublicKey(keyID string) (*rsa.PublicKey, bool) {
	k, ok := s[keyID]
	return k, ok
}

// sign mints a token the way the auth service would.
func sign(t *testing.T, key *rsa.PrivateKey, keyID string, mutate func(gojwt.MapClaims)) string {
	t.Helper()

	now := time.Now()
	claims := gojwt.MapClaims{
		"sub":   uuid.NewString(),
		"jti":   uuid.NewString(),
		"iss":   testIssuer,
		"aud":   testAudience,
		"iat":   now.Unix(),
		"exp":   now.Add(15 * time.Minute).Unix(),
		"email": "bob@example.com",
	}
	if mutate != nil {
		mutate(claims)
	}

	token := gojwt.NewWithClaims(gojwt.SigningMethodRS256, claims)
	token.Header["kid"] = keyID

	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return raw
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

func TestVerifyAcceptsAGoodToken(t *testing.T) {
	v := verifier(t, staticKeys{"dev-1": &key1.PublicKey})

	userID := uuid.New()
	raw := sign(t, key1, "dev-1", func(c gojwt.MapClaims) { c["sub"] = userID.String() })

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
		t.Error("TokenID is empty")
	}
}

func TestVerifyRejects(t *testing.T) {
	v := verifier(t, staticKeys{"dev-1": &key1.PublicKey})

	tests := map[string]struct {
		raw     string
		wantErr error
	}{
		"no token":       {raw: "", wantErr: authn.ErrNoToken},
		"garbage":        {raw: "not-a-token", wantErr: authn.ErrInvalidToken},
		"unknown key id": {raw: sign(t, key2, "dev-99", nil), wantErr: authn.ErrUnknownKeyID},
		"wrong key": {
			// Right kid, wrong signer: the signature check has to fail.
			raw: sign(t, key2, "dev-1", nil), wantErr: authn.ErrInvalidToken,
		},
		"wrong issuer": {
			raw:     sign(t, key1, "dev-1", func(c gojwt.MapClaims) { c["iss"] = "https://evil.example" }),
			wantErr: authn.ErrInvalidToken,
		},
		"wrong audience": {
			raw:     sign(t, key1, "dev-1", func(c gojwt.MapClaims) { c["aud"] = "someone-else" }),
			wantErr: authn.ErrInvalidToken,
		},
		"expired": {
			raw: sign(t, key1, "dev-1", func(c gojwt.MapClaims) {
				c["exp"] = time.Now().Add(-time.Hour).Unix()
			}),
			wantErr: authn.ErrExpired,
		},
		"subject is not a uuid": {
			raw:     sign(t, key1, "dev-1", func(c gojwt.MapClaims) { c["sub"] = "bob" }),
			wantErr: authn.ErrInvalidToken,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := v.Verify(tt.raw)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Verify error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// A token with no expiry would be valid forever.
func TestVerifyRequiresAnExpiry(t *testing.T) {
	v := verifier(t, staticKeys{"dev-1": &key1.PublicKey})

	raw := sign(t, key1, "dev-1", func(c gojwt.MapClaims) { delete(c, "exp") })

	if _, err := v.Verify(raw); err == nil {
		t.Fatal("a token without an expiry was accepted")
	}
}

// alg confusion, both variants.
func TestVerifyRejectsAlgorithmConfusion(t *testing.T) {
	v := verifier(t, staticKeys{"dev-1": &key1.PublicKey})

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
		if _, err := v.Verify(raw); err == nil {
			t.Fatal("alg=none was accepted")
		}
	})

	t.Run("hmac with the published modulus", func(t *testing.T) {
		token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims)
		token.Header["kid"] = "dev-1"

		raw, err := token.SignedString(key1.N.Bytes())
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		if _, err := v.Verify(raw); err == nil {
			t.Fatal("an HMAC token signed with the public key was accepted")
		}
	})
}

func TestBearerToken(t *testing.T) {
	tests := map[string]struct {
		header  string
		want    string
		wantErr error
	}{
		"standard":                  {header: "Bearer abc.def.ghi", want: "abc.def.ghi"},
		"lowercase scheme":          {header: "bearer abc.def.ghi", want: "abc.def.ghi"},
		"mixed case scheme":         {header: "BeArEr abc.def.ghi", want: "abc.def.ghi"},
		"absent":                    {header: "", wantErr: authn.ErrNoToken},
		"wrong scheme":              {header: "Basic dXNlcjpwYXNz", wantErr: authn.ErrInvalidToken},
		"scheme with nothing after": {header: "Bearer ", wantErr: authn.ErrInvalidToken},
		"token only":                {header: "abc.def.ghi", wantErr: authn.ErrInvalidToken},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			headers := http.Header{}
			if tt.header != "" {
				headers.Set(authn.Header, tt.header)
			}

			got, err := authn.BearerToken(headers)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("token = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClaimsRoundTripThroughContext(t *testing.T) {
	want := authn.Claims{UserID: uuid.New(), Email: "bob@example.com"}

	ctx := authn.WithClaims(context.Background(), want)

	got, ok := authn.FromContext(ctx)
	if !ok {
		t.Fatal("FromContext found no claims")
	}
	if got.UserID != want.UserID || got.Email != want.Email {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	if _, ok := authn.FromContext(context.Background()); ok {
		t.Error("FromContext reported claims on a bare context")
	}
}

// --- JWKS cache -------------------------------------------------------------

// jwksDocument renders a key set the way the auth service does.
func jwksDocument(keys map[string]*rsa.PublicKey) []byte {
	type entry struct {
		Kty string `json:"kty"`
		Use string `json:"use"`
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		N   string `json:"n"`
		E   string `json:"e"`
	}

	doc := struct {
		Keys []entry `json:"keys"`
	}{}

	for id, k := range keys {
		var e []byte
		for n := k.E; n > 0; n >>= 8 {
			e = append([]byte{byte(n & 0xff)}, e...)
		}
		doc.Keys = append(doc.Keys, entry{
			Kty: "RSA", Use: "sig", Alg: "RS256", Kid: id,
			N: base64.RawURLEncoding.EncodeToString(k.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(e),
		})
	}

	out, _ := json.Marshal(doc)
	return out
}

// jwksServer serves a key set and counts how often it was asked for one.
func jwksServer(t *testing.T, keys func() map[string]*rsa.PublicKey) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/jwk-set+json")
		_, _ = w.Write(jwksDocument(keys()))
	}))
	t.Cleanup(srv.Close)

	return srv, &hits
}

func newCache(t *testing.T, url string, cfg authn.JWKSConfig) *authn.Cache {
	t.Helper()

	cfg.URL = url
	if cfg.Logger == nil {
		cfg.Logger = logger.Discard()
	}

	c, err := authn.NewCache(cfg)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	return c
}

func TestCacheFetchesAndVerifies(t *testing.T) {
	srv, _ := jwksServer(t, func() map[string]*rsa.PublicKey {
		return map[string]*rsa.PublicKey{"dev-1": &key1.PublicKey}
	})

	cache := newCache(t, srv.URL, authn.JWKSConfig{})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	v := verifier(t, cache)

	if _, err := v.Verify(sign(t, key1, "dev-1", nil)); err != nil {
		t.Fatalf("Verify against the fetched key set: %v", err)
	}
}

// The reason the cache exists: verification must not cost a request to auth.
func TestCacheDoesNotFetchPerVerification(t *testing.T) {
	srv, hits := jwksServer(t, func() map[string]*rsa.PublicKey {
		return map[string]*rsa.PublicKey{"dev-1": &key1.PublicKey}
	})

	cache := newCache(t, srv.URL, authn.JWKSConfig{})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	v := verifier(t, cache)
	for range 50 {
		if _, err := v.Verify(sign(t, key1, "dev-1", nil)); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	}

	if got := hits.Load(); got != 1 {
		t.Fatalf("the jwks endpoint was hit %d times for 50 verifications, want 1", got)
	}
}

// A rotation publishes a new key and starts signing with it. A cache that waited
// for its refresh interval would reject every valid token until then, which is
// exactly what publishing ahead of use is meant to avoid.
func TestUnknownKeyIDTriggersARefresh(t *testing.T) {
	var rotated atomic.Bool

	srv, hits := jwksServer(t, func() map[string]*rsa.PublicKey {
		keys := map[string]*rsa.PublicKey{"dev-1": &key1.PublicKey}
		if rotated.Load() {
			keys["dev-2"] = &key2.PublicKey
		}
		return keys
	})

	cache := newCache(t, srv.URL, authn.JWKSConfig{})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	v := verifier(t, cache)

	// Signed by a key published after the cache last looked.
	rotated.Store(true)
	raw := sign(t, key2, "dev-2", nil)

	if _, err := v.Verify(raw); err != nil {
		t.Fatalf("a token from a newly rotated key was rejected: %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("the endpoint was hit %d times, want 2 (initial plus the miss)", got)
	}
}

// Without a cooldown, a stream of made-up kids turns every request into a fetch
// against the one service everything else depends on.
func TestUnknownKeyRefreshIsRateLimited(t *testing.T) {
	srv, hits := jwksServer(t, func() map[string]*rsa.PublicKey {
		return map[string]*rsa.PublicKey{"dev-1": &key1.PublicKey}
	})

	clock := time.Now()
	cache := newCache(t, srv.URL, authn.JWKSConfig{
		UnknownKeyCooldown: time.Minute,
		Now:                func() time.Time { return clock },
	})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	for range 20 {
		if _, ok := cache.PublicKey("made-up-kid"); ok {
			t.Fatal("a made-up key id resolved")
		}
	}

	// One initial fetch plus at most one triggered by the misses.
	if got := hits.Load(); got > 2 {
		t.Fatalf("the endpoint was hit %d times for 20 unknown key ids; the cooldown is not holding", got)
	}

	// Past the cooldown, one more attempt is allowed.
	clock = clock.Add(2 * time.Minute)
	cache.PublicKey("made-up-kid")

	if got := hits.Load(); got > 3 {
		t.Fatalf("the endpoint was hit %d times after the cooldown elapsed", got)
	}
}

// A failed refresh must not empty the cache: serving with slightly stale keys
// beats rejecting everything because auth is restarting.
func TestFailedRefreshKeepsTheCachedKeys(t *testing.T) {
	var broken atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if broken.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(jwksDocument(map[string]*rsa.PublicKey{"dev-1": &key1.PublicKey}))
	}))
	t.Cleanup(srv.Close)

	cache := newCache(t, srv.URL, authn.JWKSConfig{})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	broken.Store(true)
	if err := cache.Refresh(context.Background()); err == nil {
		t.Fatal("a 500 from the jwks endpoint was reported as success")
	}

	v := verifier(t, cache)
	if _, err := v.Verify(sign(t, key1, "dev-1", nil)); err != nil {
		t.Fatalf("the cached key was dropped after a failed refresh: %v", err)
	}
}

func TestParseJWKS(t *testing.T) {
	t.Run("skips entries that are not RS256 signing keys", func(t *testing.T) {
		doc := []byte(`{"keys":[
			{"kty":"RSA","use":"enc","alg":"RSA-OAEP","kid":"enc-1","n":"AQAB","e":"AQAB"},
			{"kty":"EC","use":"sig","alg":"ES256","kid":"ec-1","x":"...","y":"..."},
			{"kty":"RSA","use":"sig","alg":"RS256","kid":"good","n":"` +
			base64.RawURLEncoding.EncodeToString(key1.N.Bytes()) + `","e":"AQAB"}
		]}`)

		keys, err := authn.ParseJWKS(doc)
		if err != nil {
			t.Fatalf("ParseJWKS: %v", err)
		}
		if len(keys) != 1 {
			t.Fatalf("got %d keys, want only the RS256 signing key: %v", len(keys), keys)
		}
		if _, ok := keys["good"]; !ok {
			t.Error("the usable key was skipped")
		}
	})

	t.Run("refuses a weak modulus", func(t *testing.T) {
		short, err := rsa.GenerateKey(rand.Reader, 1024)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}

		keys, err := authn.ParseJWKS(jwksDocument(map[string]*rsa.PublicKey{"weak": &short.PublicKey}))
		if err != nil {
			t.Fatalf("ParseJWKS: %v", err)
		}
		if len(keys) != 0 {
			t.Fatal("a 1024-bit key was accepted from a JWKS document")
		}
	})

	t.Run("rejects malformed json", func(t *testing.T) {
		if _, err := authn.ParseJWKS([]byte("not json")); err == nil {
			t.Fatal("malformed JSON was accepted")
		}
	})
}

func TestNewCacheValidatesConfig(t *testing.T) {
	if _, err := authn.NewCache(authn.JWKSConfig{Logger: logger.Discard()}); err == nil {
		t.Error("a cache with no URL was constructed")
	}
	if _, err := authn.NewCache(authn.JWKSConfig{URL: "http://x"}); err == nil {
		t.Error("a cache with no logger was constructed")
	}
}

func TestNewVerifierValidatesConfig(t *testing.T) {
	keys := staticKeys{"dev-1": &key1.PublicKey}

	cases := map[string]authn.Config{
		"no keys":     {Issuer: testIssuer, Audience: testAudience},
		"no issuer":   {Keys: keys, Audience: testAudience},
		"no audience": {Keys: keys, Issuer: testIssuer},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := authn.NewVerifier(cfg); err == nil {
				t.Fatal("an invalid configuration was accepted")
			}
		})
	}
}
