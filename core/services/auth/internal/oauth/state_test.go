package oauth_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
)

// newStore returns a store over an in-process Redis, plus the server so a test
// can move its clock to force an expiry.
func newStore(t *testing.T, cfg oauth.StateStoreConfig) (*oauth.StateStore, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	store, err := oauth.NewStateStore(client, cfg)
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	return store, server
}

func sampleState() oauth.State {
	return oauth.State{
		Provider: domain.ProviderGoogle,
		Verifier: "a-verifier",
		ReturnTo: "http://localhost:3000/lobby",
	}
}

func TestStateRoundTrips(t *testing.T) {
	store, _ := newStore(t, oauth.StateStoreConfig{})

	want := sampleState()

	value, err := store.Put(t.Context(), want)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if value == "" {
		t.Fatal("Put returned an empty state value")
	}

	got, err := store.Take(t.Context(), value)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}

	if got.Provider != want.Provider {
		t.Errorf("provider = %q, want %q", got.Provider, want.Provider)
	}
	if got.Verifier != want.Verifier {
		t.Errorf("verifier = %q, want %q", got.Verifier, want.Verifier)
	}
	if got.ReturnTo != want.ReturnTo {
		t.Errorf("return_to = %q, want %q", got.ReturnTo, want.ReturnTo)
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at was not stamped")
	}
}

// The property the callback's safety rests on. A state that can be presented
// twice is a state an attacker can replay.
func TestStateIsSingleUse(t *testing.T) {
	store, _ := newStore(t, oauth.StateStoreConfig{})

	value, err := store.Put(t.Context(), sampleState())
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := store.Take(t.Context(), value); err != nil {
		t.Fatalf("the first Take failed: %v", err)
	}

	if _, err := store.Take(t.Context(), value); !errors.Is(err, domain.ErrOauthStateInvalid) {
		t.Fatalf("the second Take returned %v, want ErrOauthStateInvalid", err)
	}
}

// Two callbacks racing on one state: exactly one may win. A GET followed by a
// DEL would let both through, which is why Take uses GETDEL.
func TestConcurrentTakesHaveOneWinner(t *testing.T) {
	store, _ := newStore(t, oauth.StateStoreConfig{})

	value, err := store.Put(t.Context(), sampleState())
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	const racers = 8

	var (
		start = make(chan struct{})
		wg    sync.WaitGroup
		mu    sync.Mutex
		wins  int
	)

	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			<-start
			_, err := store.Take(t.Context(), value)

			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			}
		}()
	}

	close(start)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("%d of %d concurrent takes succeeded; exactly one must", wins, racers)
	}
}

func TestUnknownStateIsRefused(t *testing.T) {
	store, _ := newStore(t, oauth.StateStoreConfig{})

	for _, value := range []string{"", "never-issued"} {
		if _, err := store.Take(t.Context(), value); !errors.Is(err, domain.ErrOauthStateInvalid) {
			t.Errorf("Take(%q) = %v, want ErrOauthStateInvalid", value, err)
		}
	}
}

// A sign-in nobody finished must not stay completable indefinitely: an
// intercepted state should be stale long before it is useful.
func TestStateExpires(t *testing.T) {
	store, server := newStore(t, oauth.StateStoreConfig{TTL: time.Minute})

	value, err := store.Put(t.Context(), sampleState())
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	server.FastForward(time.Minute + time.Second)

	if _, err := store.Take(t.Context(), value); !errors.Is(err, domain.ErrOauthStateInvalid) {
		t.Fatalf("an expired state returned %v, want ErrOauthStateInvalid", err)
	}
}

func TestStateValuesAreUnique(t *testing.T) {
	store, _ := newStore(t, oauth.StateStoreConfig{})

	seen := make(map[string]struct{}, 50)
	for range 50 {
		value, err := store.Put(t.Context(), sampleState())
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		if _, repeat := seen[value]; repeat {
			t.Fatal("the same state value was issued twice")
		}
		seen[value] = struct{}{}
	}
}

func TestNewStateStoreRequiresAClient(t *testing.T) {
	if _, err := oauth.NewStateStore(nil, oauth.StateStoreConfig{}); err == nil {
		t.Fatal("a nil client was accepted")
	}
}
