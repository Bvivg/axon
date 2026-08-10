package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

// StateTTL is how long a started sign-in stays completable.
//
// Long enough for someone to read a consent screen and think about it, short
// enough that an intercepted state is stale before it is useful.
const StateTTL = 10 * time.Minute

// stateBytes is the entropy in a state value. It has to be unguessable: the
// whole point is that an attacker cannot forge a callback.
const stateBytes = 32

// keyPrefix namespaces these entries. Redis is shared across services.
const keyPrefix = "auth:oauth:state:"

// State is what a started sign-in remembers until its callback arrives.
type State struct {
	// Provider is recorded so a state minted for one provider cannot complete a
	// flow at another. Without it, an attacker who obtains a state for a
	// provider they control can spend it against a provider they do not.
	Provider domain.Provider `json:"provider"`

	// Verifier is the PKCE secret. It stays here and is never sent to the
	// browser, which is what makes an intercepted code useless.
	Verifier string `json:"verifier"`

	// ReturnTo has already been checked against the allow-list. Storing the
	// resolved value means the callback cannot smuggle in a different one.
	ReturnTo string `json:"return_to"`

	CreatedAt time.Time `json:"created_at"`
}

// StateStore holds in-flight sign-ins.
//
// Redis rather than Postgres: this is session state with a ten-minute lifetime,
// which is exactly what rules/infra.md says Redis is for. Losing it costs the
// sign-ins currently in flight, and those callers simply start again — nothing
// here is a source of truth.
type StateStore struct {
	client redis.UniversalClient
	ttl    time.Duration
	now    func() time.Time
}

// StateStoreConfig configures a StateStore.
type StateStoreConfig struct {
	// TTL overrides StateTTL. Tests set it; production leaves it zero.
	TTL time.Duration

	// Now overrides the clock. Tests set it; production leaves it nil.
	Now func() time.Time
}

// NewStateStore returns a store backed by client.
func NewStateStore(client redis.UniversalClient, cfg StateStoreConfig) (*StateStore, error) {
	if client == nil {
		return nil, errors.New("oauth: redis client is required")
	}

	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = StateTTL
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &StateStore{client: client, ttl: ttl, now: now}, nil
}

// Put mints a state value, stores what the callback will need, and returns it.
func (s *StateStore) Put(ctx context.Context, state State) (string, error) {
	raw := make([]byte, stateBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("oauth: generate state: %w", err)
	}
	value := base64.RawURLEncoding.EncodeToString(raw)

	state.CreatedAt = s.now().UTC()

	encoded, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("oauth: encode state: %w", err)
	}

	// SetNX rather than Set: a collision on 256 random bits does not happen, but
	// silently overwriting an unrelated in-flight sign-in if it did is not a
	// failure mode worth leaving open.
	stored, err := s.client.SetNX(ctx, keyPrefix+value, encoded, s.ttl).Result()
	if err != nil {
		return "", fmt.Errorf("oauth: store state: %w", err)
	}
	if !stored {
		return "", errors.New("oauth: state collision")
	}

	return value, nil
}

// Take consumes a state value, returning what was stored with it.
//
// Consuming is the security property, not a detail of the implementation: a
// state that could be presented twice leaves the callback open to replay. GETDEL
// reads and deletes in one operation, so two concurrent callbacks cannot both
// succeed — which a GET followed by a DEL would allow.
//
// An unknown, expired or already-spent value is one answer:
// domain.ErrOauthStateInvalid. Telling them apart would say whether a state ever
// existed, and there is nothing a caller can do differently anyway.
func (s *StateStore) Take(ctx context.Context, value string) (State, error) {
	if value == "" {
		return State{}, domain.ErrOauthStateInvalid
	}

	encoded, err := s.client.GetDel(ctx, keyPrefix+value).Bytes()
	if errors.Is(err, redis.Nil) {
		return State{}, domain.ErrOauthStateInvalid
	}
	if err != nil {
		return State{}, fmt.Errorf("oauth: take state: %w", err)
	}

	var state State
	if err := json.Unmarshal(encoded, &state); err != nil {
		// The value was ours to write, so this is corruption rather than a bad
		// caller. It still ends the sign-in, and the caller retries.
		return State{}, fmt.Errorf("oauth: decode state: %w", err)
	}

	return state, nil
}
