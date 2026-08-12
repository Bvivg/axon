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

const StateTTL = 10 * time.Minute

const stateBytes = 32

const keyPrefix = "auth:oauth:state:"

type State struct {
	Provider domain.Provider `json:"provider"`

	Verifier string `json:"verifier"`

	ReturnTo string `json:"return_to"`

	CreatedAt time.Time `json:"created_at"`
}

type StateStore struct {
	client redis.UniversalClient
	ttl    time.Duration
	now    func() time.Time
}

type StateStoreConfig struct {
	TTL time.Duration

	Now func() time.Time
}

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

	stored, err := s.client.SetNX(ctx, keyPrefix+value, encoded, s.ttl).Result()
	if err != nil {
		return "", fmt.Errorf("oauth: store state: %w", err)
	}
	if !stored {
		return "", errors.New("oauth: state collision")
	}

	return value, nil
}

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

		return State{}, fmt.Errorf("oauth: decode state: %w", err)
	}

	return state, nil
}
