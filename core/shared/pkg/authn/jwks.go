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

const (
	DefaultRefreshInterval = 5 * time.Minute

	DefaultFetchTimeout = 5 * time.Second

	DefaultUnknownKeyCooldown = 30 * time.Second

	maxJWKSBytes = 1 << 20

	DefaultStartupTimeout = 60 * time.Second

	DefaultStartupRetryInterval = 2 * time.Second
)

type JWKSConfig struct {
	URL string

	HTTPClient *http.Client

	RefreshInterval    time.Duration
	FetchTimeout       time.Duration
	UnknownKeyCooldown time.Duration

	StartupRetryInterval time.Duration

	Logger *slog.Logger

	Now func() time.Time
}

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

	lastUnknownKeyFetch time.Time

	fetching sync.Mutex
}

var _ KeySource = (*Cache)(nil)

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

func (c *Cache) KeyIDs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]string, 0, len(c.keys))
	for id := range c.keys {
		ids = append(ids, id)
	}
	return ids
}

func (c *Cache) Refresh(ctx context.Context) error {

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

func (c *Cache) Start(ctx context.Context) {
	ticker := time.NewTicker(c.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Refresh(ctx); err != nil {

				c.log.WarnContext(ctx, "scheduled jwks refresh failed, keeping the cached keys",
					"error", err)
			}
		}
	}
}

type jwk struct {
	KeyType   string `json:"kty"`
	Use       string `json:"use"`
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Modulus   string `json:"n"`
	Exponent  string `json:"e"`
}

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

		if k.Algorithm != "" && k.Algorithm != Algorithm {
			continue
		}
		if k.Use != "" && k.Use != "sig" {
			continue
		}

		key, err := parseRSAPublicKey(k.Modulus, k.Exponent)
		if err != nil {

			continue
		}
		keys[k.KeyID] = key
	}

	return keys, nil
}

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

	var exp int
	for _, b := range e {
		exp = exp<<8 | int(b)
	}
	if exp <= 0 {
		return nil, errors.New("authn: jwk exponent is not positive")
	}

	key := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exp}

	if bits := key.N.BitLen(); bits < 2048 {
		return nil, fmt.Errorf("authn: jwk modulus is %d bits, want at least 2048", bits)
	}

	return key, nil
}
