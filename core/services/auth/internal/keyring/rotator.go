package keyring

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bvivg/axon/core/services/auth/internal/jwt"
)

const purgeAfter = 24 * time.Hour

type Stored struct {
	ID          string
	Sealed      []byte
	ActivatesAt time.Time
	RetiresAt   time.Time
}

type Change struct {
	Insert      *Stored
	Retire      map[string]time.Time
	PurgeBefore time.Time
}

type Store interface {
	Load(ctx context.Context) ([]Stored, error)
	Update(ctx context.Context, decide func(now time.Time, keys []Stored) (Change, error)) error
}

type Config struct {
	Store     Store
	Sealer    *Sealer
	Ring      *jwt.Ring
	Policy    Policy
	Bootstrap jwt.PrivateKey
	Extra     []jwt.PrivateKey
	Check     time.Duration
	Log       *slog.Logger
	Now       func() time.Time
	Generate  func() (*rsa.PrivateKey, error)
}

type Rotator struct {
	cfg Config
}

func New(cfg Config) (*Rotator, error) {
	switch {
	case cfg.Store == nil, cfg.Sealer == nil, cfg.Ring == nil, cfg.Log == nil:
		return nil, errors.New("keyring: store, sealer, ring and logger are required")
	case cfg.Policy.Interval <= 0:
		return nil, errors.New("keyring: rotation interval must be positive")
	case cfg.Policy.Lead < 0 || cfg.Policy.Retain <= 0:
		return nil, errors.New("keyring: lead must not be negative and retain must be positive")
	case cfg.Bootstrap.Key == nil || cfg.Bootstrap.ID == "":
		return nil, errors.New("keyring: a bootstrap key is required")
	}
	if cfg.Check <= 0 {
		cfg.Check = 30 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Generate == nil {
		cfg.Generate = func() (*rsa.PrivateKey, error) { return rsa.GenerateKey(rand.Reader, jwt.MinKeySizeBits) }
	}
	return &Rotator{cfg: cfg}, nil
}

func (r *Rotator) Run(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.Check)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := r.Sync(ctx); err != nil && ctx.Err() == nil {
			r.cfg.Log.WarnContext(ctx, "signing key rotation failed", "error", err)
		}
	}
}

func (r *Rotator) Sync(ctx context.Context) error {
	var created string
	err := r.cfg.Store.Update(ctx, func(now time.Time, stored []Stored) (Change, error) {
		created = ""
		keys, err := r.open(stored)
		if err != nil {
			return Change{}, err
		}
		if len(keys) == 0 {
			sealed, err := r.seal(r.cfg.Bootstrap.ID, r.cfg.Bootstrap.Key)
			if err != nil {
				return Change{}, err
			}
			created = r.cfg.Bootstrap.ID
			return Change{Insert: &Stored{ID: r.cfg.Bootstrap.ID, Sealed: sealed, ActivatesAt: now}}, nil
		}
		change := Change{PurgeBefore: now.Add(-purgeAfter)}
		if !Due(keys, now, r.cfg.Policy.Interval) {
			return change, nil
		}
		key, err := r.cfg.Generate()
		if err != nil {
			return Change{}, fmt.Errorf("keyring: generate key: %w", err)
		}
		id, err := newKeyID(now)
		if err != nil {
			return Change{}, err
		}
		sealed, err := r.seal(id, key)
		if err != nil {
			return Change{}, err
		}
		activates, retire := Successor(keys, now, r.cfg.Policy)
		change.Insert = &Stored{ID: id, Sealed: sealed, ActivatesAt: activates}
		change.Retire = retire
		created = id
		return change, nil
	})
	if err != nil {
		return err
	}
	if created != "" {
		r.cfg.Log.InfoContext(ctx, "signing key created", "kid", created)
	}
	return r.Refresh(ctx)
}

func (r *Rotator) Refresh(ctx context.Context) error {
	stored, err := r.cfg.Store.Load(ctx)
	if err != nil {
		return err
	}
	keys, err := r.open(stored)
	if err != nil {
		return err
	}
	set, err := Set(keys, r.cfg.Now(), r.cfg.Extra...)
	if err != nil {
		return err
	}
	if previous := r.cfg.Ring.Current(); previous == nil || previous.Active().ID != set.Active().ID {
		r.cfg.Log.InfoContext(ctx, "signing key active", "kid", set.Active().ID, "published", set.KeyIDs())
	}
	r.cfg.Ring.Replace(set)
	return nil
}

func (r *Rotator) open(stored []Stored) ([]Key, error) {
	keys := make([]Key, 0, len(stored))
	for _, s := range stored {
		der, err := r.cfg.Sealer.Open(s.Sealed, s.ID)
		if err != nil {
			return nil, fmt.Errorf("keyring: key %q: %w", s.ID, err)
		}
		parsed, err := x509.ParsePKCS8PrivateKey(der)
		if err != nil {
			return nil, fmt.Errorf("keyring: key %q: %w", s.ID, err)
		}
		private, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("keyring: key %q is not RSA", s.ID)
		}
		keys = append(keys, Key{ID: s.ID, Private: private, ActivatesAt: s.ActivatesAt, RetiresAt: s.RetiresAt})
	}
	return keys, nil
}

func (r *Rotator) seal(id string, key *rsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("keyring: marshal key: %w", err)
	}
	return r.cfg.Sealer.Seal(der, id)
}

func newKeyID(now time.Time) (string, error) {
	suffix := make([]byte, 3)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("keyring: key id: %w", err)
	}
	return "k" + now.UTC().Format("20060102T150405") + "-" + hex.EncodeToString(suffix), nil
}
