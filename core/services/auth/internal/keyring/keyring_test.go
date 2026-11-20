package keyring_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/auth/internal/jwt"
	"github.com/bvivg/axon/core/services/auth/internal/keyring"
)

var (
	keyOnce sync.Once
	keyPool []*rsa.PrivateKey
)

func rsaKey(t *testing.T, n int) *rsa.PrivateKey {
	t.Helper()
	keyOnce.Do(func() {
		for range 6 {
			k, err := rsa.GenerateKey(rand.Reader, jwt.MinKeySizeBits)
			if err != nil {
				panic(err)
			}
			keyPool = append(keyPool, k)
		}
	})
	return keyPool[n%len(keyPool)]
}

var t0 = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func TestDue(t *testing.T) {
	cases := []struct {
		name string
		keys []keyring.Key
		now  time.Time
		want bool
	}{
		{"empty ring", nil, t0, true},
		{"fresh key", []keyring.Key{{ID: "a", ActivatesAt: t0}}, t0.Add(time.Hour), false},
		{"interval elapsed", []keyring.Key{{ID: "a", ActivatesAt: t0}}, t0.Add(24 * time.Hour), true},
		{"successor pending", []keyring.Key{{ID: "a", ActivatesAt: t0}, {ID: "b", ActivatesAt: t0.Add(25 * time.Hour)}}, t0.Add(24 * time.Hour), false},
		{"newest active decides", []keyring.Key{{ID: "a", ActivatesAt: t0, RetiresAt: t0.Add(26 * time.Hour)}, {ID: "b", ActivatesAt: t0.Add(24 * time.Hour)}}, t0.Add(25 * time.Hour), false},
		{"only retired keys", []keyring.Key{{ID: "a", ActivatesAt: t0, RetiresAt: t0.Add(time.Hour)}}, t0.Add(2 * time.Hour), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := keyring.Due(tc.keys, tc.now, 24*time.Hour); got != tc.want {
				t.Fatalf("Due = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSuccessorRetiresEveryOpenKeyAfterTheTokenLifetime(t *testing.T) {
	policy := keyring.Policy{Interval: time.Hour, Lead: 10 * time.Minute, Retain: 16 * time.Minute}
	keys := []keyring.Key{
		{ID: "old", ActivatesAt: t0.Add(-2 * time.Hour), RetiresAt: t0.Add(-time.Hour)},
		{ID: "current", ActivatesAt: t0.Add(-time.Hour)},
	}
	activates, retire := keyring.Successor(keys, t0, policy)
	if !activates.Equal(t0.Add(10 * time.Minute)) {
		t.Fatal("successor must activate after the lead", activates)
	}
	if len(retire) != 1 || !retire["current"].Equal(t0.Add(26*time.Minute)) {
		t.Fatal("current key must retire one token lifetime after the switch", retire)
	}
}

func TestSetPublishesPendingKeysButSignsWithTheNewestActive(t *testing.T) {
	keys := []keyring.Key{
		{ID: "retired", Private: rsaKey(t, 0), ActivatesAt: t0.Add(-3 * time.Hour), RetiresAt: t0.Add(-time.Minute)},
		{ID: "previous", Private: rsaKey(t, 1), ActivatesAt: t0.Add(-2 * time.Hour), RetiresAt: t0.Add(time.Minute)},
		{ID: "current", Private: rsaKey(t, 2), ActivatesAt: t0.Add(-time.Hour)},
		{ID: "pending", Private: rsaKey(t, 3), ActivatesAt: t0.Add(time.Minute)},
	}
	set, err := keyring.Set(keys, t0, jwt.PrivateKey{ID: "legacy", Key: rsaKey(t, 4)}, jwt.PrivateKey{ID: "current", Key: rsaKey(t, 5)})
	if err != nil {
		t.Fatal(err)
	}
	if set.Active().ID != "current" {
		t.Fatal("active key", set.Active().ID)
	}
	if got, want := set.KeyIDs(), []string{"current", "legacy", "pending", "previous"}; !slices.Equal(got, want) {
		t.Fatal("published keys", got, want)
	}
	if pub, _ := set.PublicKey("current"); !pub.Equal(&rsaKey(t, 2).PublicKey) {
		t.Fatal("an extra key with the same id must not replace the stored one")
	}
	if _, err := keyring.Set(keys[3:], t0); !errors.Is(err, keyring.ErrNoActiveKey) {
		t.Fatal("a ring with only pending keys has nothing to sign with", err)
	}
}

func TestSealerBindsTheKeyID(t *testing.T) {
	sealer, err := keyring.NewSealer(bytes.Repeat([]byte{7}, keyring.EncryptionKeySize))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := sealer.Seal([]byte("secret"), "a")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("secret")) {
		t.Fatal("plaintext leaked into the sealed value")
	}
	if got, err := sealer.Open(sealed, "a"); err != nil || string(got) != "secret" {
		t.Fatal(got, err)
	}
	if _, err := sealer.Open(sealed, "b"); !errors.Is(err, keyring.ErrSealed) {
		t.Fatal("a sealed key moved to another id must not open", err)
	}
	other, _ := keyring.NewSealer(bytes.Repeat([]byte{8}, keyring.EncryptionKeySize))
	if _, err := other.Open(sealed, "a"); !errors.Is(err, keyring.ErrSealed) {
		t.Fatal("another encryption key must not open it", err)
	}
	if _, err := keyring.NewSealer([]byte("short")); err == nil {
		t.Fatal("short encryption key accepted")
	}
}

type memory struct {
	mu   sync.Mutex
	now  *time.Time
	rows map[string]keyring.Stored
}

func (m *memory) Load(context.Context) ([]keyring.Stored, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.live(*m.now), nil
}

func (m *memory) live(now time.Time) []keyring.Stored {
	var out []keyring.Stored
	for _, s := range m.rows {
		if s.RetiresAt.IsZero() || s.RetiresAt.After(now) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ActivatesAt.Before(out[j].ActivatesAt) })
	return out
}

func (m *memory) Update(_ context.Context, decide func(time.Time, []keyring.Stored) (keyring.Change, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := *m.now
	change, err := decide(now, m.live(now))
	if err != nil {
		return err
	}
	for id, at := range change.Retire {
		if row := m.rows[id]; row.RetiresAt.IsZero() {
			row.RetiresAt = at
			m.rows[id] = row
		}
	}
	if change.Insert != nil {
		m.rows[change.Insert.ID] = *change.Insert
	}
	for id, row := range m.rows {
		if !row.RetiresAt.IsZero() && row.RetiresAt.Before(change.PurgeBefore) {
			delete(m.rows, id)
		}
	}
	return nil
}

func TestRotationKeepsEveryIssuedTokenVerifiable(t *testing.T) {
	now := t0
	store := &memory{now: &now, rows: map[string]keyring.Stored{}}
	sealer, _ := keyring.NewSealer(bytes.Repeat([]byte{1}, keyring.EncryptionKeySize))
	bootstrap := jwt.PrivateKey{ID: "env", Key: rsaKey(t, 0)}
	initial, _ := jwt.NewKeySet(bootstrap)
	ring := jwt.NewRing(initial)
	generated := 0
	accessTTL := 15 * time.Minute
	rotator, err := keyring.New(keyring.Config{
		Store: store, Sealer: sealer, Ring: ring, Bootstrap: bootstrap,
		Policy: keyring.Policy{Interval: time.Hour, Lead: 10 * time.Minute, Retain: accessTTL + time.Minute},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    func() time.Time { return now },
		Generate: func() (*rsa.PrivateKey, error) {
			generated++
			return rsaKey(t, generated), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := jwt.NewIssuer(jwt.IssuerConfig{Keys: ring, Issuer: "https://auth.axon.test", Audience: "axon", TTL: accessTTL, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := authn.NewVerifier(authn.Config{Keys: ring, Issuer: "https://auth.axon.test", Audience: "axon", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	type issued struct {
		raw     string
		expires time.Time
	}
	var tokens []issued
	signers := map[string]bool{}
	for step := 0; step < 6*60*2; step++ {
		if err := rotator.Sync(t.Context()); err != nil {
			t.Fatal(err)
		}
		signers[ring.Active().ID] = true
		raw, expires, err := issuer.Issue(uuid.New(), "rotation@axon.test", uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, issued{raw: raw, expires: expires})
		live := tokens[:0]
		for _, token := range tokens {
			if !token.expires.After(now) {
				continue
			}
			if _, err := verifier.Verify(token.raw); err != nil {
				t.Fatalf("at %s a live token no longer verifies: %v", now.Sub(t0), err)
			}
			live = append(live, token)
		}
		tokens = live
		current := ring.Current()
		if len(current.KeyIDs()) > 3 {
			t.Fatalf("at %s the JWKS carries %v", now.Sub(t0), current.KeyIDs())
		}
		now = now.Add(30 * time.Second)
	}
	if len(signers) < 6 {
		t.Fatal("six hours at a one-hour interval must sign with at least six keys", len(signers))
	}
	if len(store.rows) > 26 {
		t.Fatal("retired keys are never purged", len(store.rows))
	}
}
