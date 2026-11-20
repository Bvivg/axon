//go:build integration

package integration

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/bvivg/axon/core/services/auth/internal/jwt"
	"github.com/bvivg/axon/core/services/auth/internal/keyring"
)

func TestConcurrentInstancesCreateOneSigningKeyPerInterval(t *testing.T) {
	ctx := t.Context()
	if _, err := pool.Exec(ctx, `DELETE FROM signing_keys`); err != nil {
		t.Fatal(err)
	}
	bootstrapKey, err := rsa.GenerateKey(rand.Reader, jwt.MinKeySizeBits)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := jwt.PrivateKey{ID: "env-bootstrap", Key: bootstrapKey}
	sealer, err := keyring.NewSealer(bytes.Repeat([]byte{3}, keyring.EncryptionKeySize))
	if err != nil {
		t.Fatal(err)
	}
	store := keyring.NewPostgres(pool.Pool)
	initial, _ := jwt.NewKeySet(bootstrap)
	instances := make([]*keyring.Rotator, 4)
	rings := make([]*jwt.Ring, len(instances))
	for i := range instances {
		rings[i] = jwt.NewRing(initial)
		instances[i], err = keyring.New(keyring.Config{
			Store: store, Sealer: sealer, Ring: rings[i], Bootstrap: bootstrap,
			Policy: keyring.Policy{Interval: 2 * time.Second, Lead: 500 * time.Millisecond, Retain: time.Second},
			Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sync := func() {
		var wg sync.WaitGroup
		for _, r := range instances {
			wg.Go(func() {
				if err := r.Sync(ctx); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
	}
	sync()
	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM signing_keys`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(); n != 1 {
		t.Fatal("bootstrap must be stored once", n)
	}
	for _, ring := range rings {
		if ring.Active().ID != "env-bootstrap" {
			t.Fatal("instances disagree on the active key", ring.Active().ID)
		}
	}
	time.Sleep(2100 * time.Millisecond)
	sync()
	if n := count(); n != 2 {
		t.Fatal("one successor per interval across instances", n)
	}
	for _, ring := range rings {
		if ring.Active().ID != "env-bootstrap" || len(ring.Current().KeyIDs()) != 2 {
			t.Fatal("successor must be published before it signs", ring.Active().ID, ring.Current().KeyIDs())
		}
	}
	time.Sleep(600 * time.Millisecond)
	sync()
	successor := rings[0].Active().ID
	if successor == "env-bootstrap" {
		t.Fatal("successor did not activate after the lead")
	}
	for _, ring := range rings {
		if ring.Active().ID != successor {
			t.Fatal("instances disagree after activation", ring.Active().ID, successor)
		}
	}
	time.Sleep(1100 * time.Millisecond)
	for _, r := range instances {
		if err := r.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if ids := rings[0].Current().KeyIDs(); len(ids) != 1 || ids[0] != successor {
		t.Fatal("bootstrap key must leave the JWKS after it retires", ids)
	}
	var sealed []byte
	if err := pool.QueryRow(ctx, `SELECT private_key FROM signing_keys WHERE kid = $1`, successor).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("PRIVATE KEY")) || len(sealed) < 1000 {
		t.Fatal("stored key is not sealed")
	}
}
