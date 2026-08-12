package authn

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// JWKS defaults.
const (
	// DefaultRefreshInterval is how often the key set is re-fetched in the
	// background. Short enough that a rotation propagates on its own, long
	// enough that the auth service is not being polled constantly.
	DefaultRefreshInterval = 5 * time.Minute

	// DefaultFetchTimeout bounds one fetch.
	DefaultFetchTimeout = 5 * time.Second

	// DefaultUnknownKeyCooldown is the minimum gap between refreshes triggered
	// by a token naming a key the cache does not have. Without it, a stream of
	// tokens carrying made-up kids turns every request into a fetch against the
	// auth service — a free amplifier pointed at the one service everything else
	// depends on.
	DefaultUnknownKeyCooldown = 30 * time.Second

	// maxJWKSBytes caps the response. A key set is a few kilobytes; anything
	// larger is a misconfiguration or something hostile, and either way should
	// not be read into memory.
	maxJWKSBytes = 1 << 20

	// DefaultStartupTimeout bounds WaitUntilReady.
	//
	// Long enough that auth restarted by the same event that restarted this
	// process — a Docker Desktop restart or a host reboot, not a `docker compose
	// up` — has time to become reachable too; short enough that a URL which is
	// actually wrong still fails startup rather than hanging indefinitely.
	DefaultStartupTimeout = 60 * time.Second

	// DefaultStartupRetryInterval paces the attempts inside WaitUntilReady.
	DefaultStartupRetryInterval = 2 * time.Second
)

// JWKSConfig configures a Cache.
type JWKSConfig struct {
	// URL is the JWKS endpoint, e.g. http://auth:9091/.well-known/jwks.json
	URL string

	// HTTPClient is used for fetches. Defaults to a client with FetchTimeout.
	HTTPClient *http.Client

	RefreshInterval    time.Duration
	FetchTimeout       time.Duration
	UnknownKeyCooldown time.Duration

	// StartupRetryInterval paces WaitUntilReady's attempts. Tests shrink it;
	// production leaves it at DefaultStartupRetryInterval.
	StartupRetryInterval time.Duration

	Logger *slog.Logger

	// Now overrides the clock. Tests set it; production leaves it nil.
	Now func() time.Time
}

// Cache holds the published key set and keeps it current.
//
// It exists so verification stays local. Asking the auth service about every
// request would put it in the path of all traffic and undo the reason for
// choosing asymmetric signing in the first place.
type Cache struct {
	url                  string
	client               *http.Client
	refreshInterval      time.Duration
	unknownKeyCooldown   time.Duration
	startupRetryInterval time.Duration
	log                  *slog.Logger
	now                  func() time.Time

	mu   sync.RWMutex
	keys map[string]*rsa.PublicKey
	// lastUnknownKeyFetch rate-limits refreshes triggered by unknown kids.
	lastUnknownKeyFetch time.Time

	// fetching collapses concurrent refreshes into one request, so a burst of
	// misses does not become a burst of fetches.
	fetching sync.Mutex
}

var _ KeySource = (*Cache)(nil)

// NewCache builds a Cache. It does not fetch: call WaitUntilReady once before
// serving so a misconfigured URL still fails startup, then Start for the
// background loop.
func NewCache(cfg JWKSConfig) (*Cache, error) {
	if cfg.URL == "" {
		return nil, errors.New("authn: jwks cache needs a URL")
	}
	if cfg.Logger == nil {
		return nil, errors.New("authn: jwks cache needs a logger")
	}

	fetchTimeout := cfg.FetchTimeout
	if fetchTimeout <= 0 {
		fetchTimeout = DefaultFetchTimeout
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout}
	}

	refresh := cfg.RefreshInterval
	if refresh <= 0 {
		refresh = DefaultRefreshInterval
	}

	cooldown := cfg.UnknownKeyCooldown
	if cooldown <= 0 {
		cooldown = DefaultUnknownKeyCooldown
	}

	startupRetry := cfg.StartupRetryInterval
	if startupRetry <= 0 {
		startupRetry = DefaultStartupRetryInterval
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Cache{
		url:                  cfg.URL,
		client:               client,
		refreshInterval:      refresh,
		unknownKeyCooldown:   cooldown,
		startupRetryInterval: startupRetry,
		log:                  cfg.Logger,
		now:                  now,
		keys:                 make(map[string]*rsa.PublicKey),
	}, nil
}

// PublicKey returns the key published under keyID.
//
// A miss triggers a refresh, because the usual cause is a rotation: the auth
// service began signing with a key published after this cache last looked. The
// alternative — waiting for the interval — would reject every valid token for up
// to that long on each rotation, which is exactly what publishing keys ahead of
// use is supposed to prevent.
//
// The refresh is on a cooldown so this cannot be used to make a service hammer
// auth by sending garbage kids.
func (c *Cache) PublicKey(keyID string) (*rsa.PublicKey, bool) {
	if key, ok := c.lookup(keyID); ok {
		return key, true
	}

	if !c.mayFetchForUnknownKey() {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultFetchTimeout)
	defer cancel()

	c.log.InfoContext(ctx, "refreshing jwks after an unknown key id", "kid", keyID)

	if err := c.Refresh(ctx); err != nil {
		c.log.ErrorContext(ctx, "could not refresh jwks", "error", err)
		return nil, false
	}

	return c.lookup(keyID)
}

func (c *Cache) lookup(keyID string) (*rsa.PublicKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key, ok := c.keys[keyID]
	return key, ok
}

// mayFetchForUnknownKey reports whether the cooldown has elapsed, and starts a
// new one when it has.
func (c *Cache) mayFetchForUnknownKey() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if now.Sub(c.lastUnknownKeyFetch) < c.unknownKeyCooldown {
		return false
	}
	c.lastUnknownKeyFetch = now
	return true
}

// KeyIDs returns the cached key ids. Used by readiness and diagnostics.
func (c *Cache) KeyIDs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]string, 0, len(c.keys))
	for id := range c.keys {
		ids = append(ids, id)
	}
	return ids
}

// Refresh fetches the key set and replaces the cache.
//
// A failed fetch leaves the previous keys in place. Serving with a slightly
// stale set beats rejecting every request because auth happened to be
// restarting.
func (c *Cache) Refresh(ctx context.Context) error {
	// One fetch at a time: a burst of misses should cost one request.
	c.fetching.Lock()
	defer c.fetching.Unlock()

	keys, err := c.fetch(ctx)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return errors.New("authn: jwks document contains no usable keys")
	}

	c.mu.Lock()
	c.keys = keys
	c.mu.Unlock()

	return nil
}

func (c *Cache) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("authn: build jwks request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("authn: fetch jwks: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authn: jwks endpoint returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBytes))
	if err != nil {
		return nil, fmt.Errorf("authn: read jwks: %w", err)
	}

	return ParseJWKS(body)
}

// WaitUntilReady fetches the key set, retrying until it succeeds or timeout
// elapses.
//
// This is what a service should call at startup instead of a bare Refresh.
// depends_on's service_healthy condition only orders a `docker compose up`; it
// does nothing when the daemon itself restarts already-running containers —
// a Docker Desktop restart, a host reboot — which brings every service with a
// `restart: unless-stopped` policy back independently, in no particular order.
// auth answering slower than this one, or not yet at all, is therefore the
// routine case here, not a misconfiguration, and one failed attempt must not
// be fatal. A URL that is genuinely wrong still surfaces as an error — just
// after this budget elapses instead of on the first try.
func (c *Cache) WaitUntilReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for {
		lastErr = c.Refresh(ctx)
		if lastErr == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("authn: jwks was not reachable within %s: %w", timeout, lastErr)
		case <-time.After(c.startupRetryInterval):
		}
	}
}

// Start runs the background refresh until ctx is cancelled.
func (c *Cache) Start(ctx context.Context) {
	ticker := time.NewTicker(c.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Refresh(ctx); err != nil {
				// Warn, not error: the previous keys are still serving, so this
				// is an anomaly to watch rather than an outage.
				c.log.WarnContext(ctx, "scheduled jwks refresh failed, keeping the cached keys",
					"error", err)
			}
		}
	}
}

// jwk is one entry of an RFC 7517 document.
type jwk struct {
	KeyType   string `json:"kty"`
	Use       string `json:"use"`
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Modulus   string `json:"n"`
	Exponent  string `json:"e"`
}

// ParseJWKS reads an RFC 7517 document into public keys by key id.
//
// Entries that are not RS256 signing keys are skipped rather than rejected: a
// key set may legitimately carry keys for other purposes, and refusing the whole
// document over one of them would take the service down for no reason.
func ParseJWKS(data []byte) (map[string]*rsa.PublicKey, error) {
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("authn: parse jwks: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))

	for _, k := range doc.Keys {
		if k.KeyType != "RSA" || k.KeyID == "" {
			continue
		}
		// An empty alg is allowed — it is optional in RFC 7517 — but a stated
		// one that is not RS256 means the key is not for us.
		if k.Algorithm != "" && k.Algorithm != Algorithm {
			continue
		}
		if k.Use != "" && k.Use != "sig" {
			continue
		}

		key, err := parseRSAPublicKey(k.Modulus, k.Exponent)
		if err != nil {
			// One malformed entry does not invalidate the rest.
			continue
		}
		keys[k.KeyID] = key
	}

	return keys, nil
}

// parseRSAPublicKey rebuilds a public key from the base64url modulus and
// exponent of RFC 7518.
func parseRSAPublicKey(modulus, exponent string) (*rsa.PublicKey, error) {
	if modulus == "" || exponent == "" {
		return nil, errors.New("authn: jwk is missing its modulus or exponent")
	}

	n, err := base64.RawURLEncoding.DecodeString(modulus)
	if err != nil {
		return nil, fmt.Errorf("authn: decode modulus: %w", err)
	}

	e, err := base64.RawURLEncoding.DecodeString(exponent)
	if err != nil {
		return nil, fmt.Errorf("authn: decode exponent: %w", err)
	}
	if len(e) == 0 || len(e) > 8 {
		return nil, errors.New("authn: jwk exponent is out of range")
	}

	// Big-endian, shortest form: 65537 arrives as AQAB, three bytes.
	var exp int
	for _, b := range e {
		exp = exp<<8 | int(b)
	}
	if exp <= 0 {
		return nil, errors.New("authn: jwk exponent is not positive")
	}

	key := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exp}

	// Same floor the issuer enforces, so a downgraded key set cannot talk a
	// verifier into accepting signatures it would never have issued.
	if bits := key.N.BitLen(); bits < 2048 {
		return nil, fmt.Errorf("authn: jwk modulus is %d bits, want at least 2048", bits)
	}

	return key, nil
}
