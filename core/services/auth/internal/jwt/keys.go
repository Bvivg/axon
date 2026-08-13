package jwt

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"sort"

	"github.com/bvivg/axon/core/shared/pkg/authn"
)

const MinKeySizeBits = 2048

type PrivateKey struct {
	ID  string
	Key *rsa.PrivateKey
}

func ParsePrivateKeyPEM(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("jwt: no PEM block found in key material")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return validateKeySize(key)
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {

		return nil, errors.New("jwt: key is neither a PKCS#1 nor a PKCS#8 RSA private key")
	}

	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("jwt: key is %T, want an RSA private key", parsed)
	}
	return validateKeySize(key)
}

func validateKeySize(key *rsa.PrivateKey) (*rsa.PrivateKey, error) {
	if bits := key.N.BitLen(); bits < MinKeySizeBits {
		return nil, fmt.Errorf("jwt: RSA key is %d bits, want at least %d", bits, MinKeySizeBits)
	}
	return key, nil
}

type KeySet struct {
	active  PrivateKey
	byKeyID map[string]*rsa.PublicKey
}

var _ authn.KeySource = (*KeySet)(nil)

func NewKeySet(active PrivateKey, additional ...PrivateKey) (*KeySet, error) {
	if active.ID == "" {
		return nil, errors.New("jwt: the active key has no id")
	}
	if active.Key == nil {
		return nil, errors.New("jwt: the active key has no key material")
	}
	if _, err := validateKeySize(active.Key); err != nil {
		return nil, err
	}

	set := &KeySet{
		active:  active,
		byKeyID: map[string]*rsa.PublicKey{active.ID: &active.Key.PublicKey},
	}

	for _, k := range additional {
		if k.ID == "" {
			return nil, errors.New("jwt: an additional key has no id")
		}
		if k.Key == nil {
			return nil, fmt.Errorf("jwt: additional key %q has no key material", k.ID)
		}
		if _, err := validateKeySize(k.Key); err != nil {
			return nil, fmt.Errorf("jwt: additional key %q: %w", k.ID, err)
		}
		set.byKeyID[k.ID] = &k.Key.PublicKey
	}

	return set, nil
}

func (s *KeySet) Active() PrivateKey { return s.active }

func (s *KeySet) PublicKey(keyID string) (*rsa.PublicKey, bool) {
	key, ok := s.byKeyID[keyID]
	return key, ok
}

func (s *KeySet) KeyIDs() []string {
	ids := make([]string, 0, len(s.byKeyID))
	for id := range s.byKeyID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

type jwk struct {
	KeyType   string `json:"kty"`
	Use       string `json:"use"`
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`

	Modulus  string `json:"n"`
	Exponent string `json:"e"`
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

func (s *KeySet) JWKS() ([]byte, error) {
	doc := jwks{Keys: make([]jwk, 0, len(s.byKeyID))}

	for _, id := range s.KeyIDs() {
		pub := s.byKeyID[id]

		doc.Keys = append(doc.Keys, jwk{
			KeyType:   "RSA",
			Use:       "sig",
			Algorithm: "RS256",
			KeyID:     id,
			Modulus:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			Exponent:  base64.RawURLEncoding.EncodeToString(bigEndian(pub.E)),
		})
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("jwt: marshal jwks: %w", err)
	}
	return out, nil
}

func bigEndian(n int) []byte {
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte(n & 0xff)}, buf...)
		n >>= 8
	}
	if len(buf) == 0 {
		return []byte{0}
	}
	return buf
}
