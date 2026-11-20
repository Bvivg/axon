package jwt

import (
	"crypto/rsa"
	"sync/atomic"
)

type Keys interface {
	Active() PrivateKey
	PublicKey(keyID string) (*rsa.PublicKey, bool)
	JWKS() ([]byte, error)
}

var (
	_ Keys = (*KeySet)(nil)
	_ Keys = (*Ring)(nil)
)

type Ring struct {
	current atomic.Pointer[KeySet]
}

func NewRing(initial *KeySet) *Ring {
	r := &Ring{}
	r.current.Store(initial)
	return r
}

func (r *Ring) Replace(set *KeySet) {
	if set != nil {
		r.current.Store(set)
	}
}

func (r *Ring) Current() *KeySet { return r.current.Load() }

func (r *Ring) Active() PrivateKey { return r.current.Load().Active() }

func (r *Ring) PublicKey(keyID string) (*rsa.PublicKey, bool) {
	return r.current.Load().PublicKey(keyID)
}

func (r *Ring) JWKS() ([]byte, error) { return r.current.Load().JWKS() }
