package keyring

import (
	"crypto/rsa"
	"errors"
	"time"

	"github.com/bvivg/axon/core/services/auth/internal/jwt"
)

type Key struct {
	ID          string
	Private     *rsa.PrivateKey
	ActivatesAt time.Time
	RetiresAt   time.Time
}

func (k Key) live(now time.Time) bool { return k.RetiresAt.IsZero() || k.RetiresAt.After(now) }

type Policy struct {
	Interval time.Duration
	Lead     time.Duration
	Retain   time.Duration
}

func Due(keys []Key, now time.Time, interval time.Duration) bool {
	var latest time.Time
	for _, k := range keys {
		if !k.live(now) {
			continue
		}
		if k.ActivatesAt.After(latest) {
			latest = k.ActivatesAt
		}
	}
	if latest.IsZero() {
		return true
	}
	return !latest.After(now) && now.Sub(latest) >= interval
}

func Successor(keys []Key, now time.Time, policy Policy) (time.Time, map[string]time.Time) {
	activates := now.Add(policy.Lead)
	retire := map[string]time.Time{}
	for _, k := range keys {
		if k.RetiresAt.IsZero() {
			retire[k.ID] = activates.Add(policy.Retain)
		}
	}
	return activates, retire
}

var ErrNoActiveKey = errors.New("keyring: no key is active yet")

func Set(keys []Key, now time.Time, extra ...jwt.PrivateKey) (*jwt.KeySet, error) {
	var active *Key
	var others []jwt.PrivateKey
	for i := range keys {
		k := &keys[i]
		if !k.live(now) {
			continue
		}
		if k.ActivatesAt.After(now) {
			others = append(others, jwt.PrivateKey{ID: k.ID, Key: k.Private})
			continue
		}
		if active == nil || k.ActivatesAt.After(active.ActivatesAt) {
			if active != nil {
				others = append(others, jwt.PrivateKey{ID: active.ID, Key: active.Private})
			}
			active = k
			continue
		}
		others = append(others, jwt.PrivateKey{ID: k.ID, Key: k.Private})
	}
	if active == nil {
		return nil, ErrNoActiveKey
	}
	seen := map[string]bool{active.ID: true}
	for _, k := range others {
		seen[k.ID] = true
	}
	for _, k := range extra {
		if !seen[k.ID] {
			others = append(others, k)
			seen[k.ID] = true
		}
	}
	return jwt.NewKeySet(jwt.PrivateKey{ID: active.ID, Key: active.Private}, others...)
}
