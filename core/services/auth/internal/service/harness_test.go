package service_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/jwt"
	"github.com/bvivg/axon/core/services/auth/internal/password"
	"github.com/bvivg/axon/core/services/auth/internal/service"
	"github.com/bvivg/axon/core/services/auth/internal/token"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

const (
	validPassword = "correct horse battery staple"
	refreshTTL    = 720 * time.Hour
	accessTTL     = 15 * time.Minute
)

var signingKey *rsa.PrivateKey

func init() {
	var err error
	if signingKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		panic(err)
	}
}

type harness struct {
	svc   *service.Service
	store *fakeStore

	clock time.Time
}

type harnessOption func(*service.Config)

func newHarness(t *testing.T, opts ...harnessOption) *harness {
	t.Helper()

	h := &harness{
		store: newFakeStore(),
		clock: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}

	params := password.DefaultParams()
	params.Memory = 8 * 1024
	params.Time = 1

	hasher, err := password.NewHasher(params)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}

	keys, err := jwt.NewKeySet(jwt.PrivateKey{ID: "test-1", Key: signingKey})
	if err != nil {
		t.Fatalf("NewKeySet: %v", err)
	}

	issuer, err := jwt.NewIssuer(jwt.IssuerConfig{
		Keys:     keys,
		Issuer:   "https://auth.axon.test",
		Audience: "axon",
		TTL:      accessTTL,
		Now:      h.now,
	})
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	cfg := service.Config{
		RefreshTTL: refreshTTL,
		Now:        h.now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	svc, err := service.New(h.store, hasher, issuer, logger.Discard(), cfg)
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}

	h.svc = svc
	return h
}

func (h *harness) now() time.Time { return h.clock }

func (h *harness) advance(d time.Duration) { h.clock = h.clock.Add(d) }

func (h *harness) register(t *testing.T, email, pw string) service.Result {
	t.Helper()

	res, err := h.svc.Register(context.Background(), service.RegisterInput{
		Email:    email,
		Password: pw,
	})
	if err != nil {
		t.Fatalf("Register(%s): %v", email, err)
	}
	return res
}

func (h *harness) storedToken(t *testing.T, value string) domain.RefreshToken {
	t.Helper()

	hash, err := token.Hash(value)
	if err != nil {
		t.Fatalf("token.Hash: %v", err)
	}

	stored, ok := h.store.tokenByHash(hash)
	if !ok {
		t.Fatalf("no stored token for the value the service returned")
	}
	return stored
}
