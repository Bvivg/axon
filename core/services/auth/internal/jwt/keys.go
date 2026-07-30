// Package jwt issues and verifies the RS256 access tokens the rest of the
// system trusts.
//
// Asymmetric signing is the point: every service verifies a token with the
// public key it fetched from the JWKS endpoint, so a request never costs a round
// trip to auth, and no service but auth can mint a token.
//
// Key rotation is designed in from the start rather than retrofitted. The key
// set holds several keys at once and every token names the one that signed it in
// its `kid` header. Rotating means publishing the new key first, letting clients
// pick it up, and only then switching signing over to it. A single-key setup
// would make that impossible: the moment signing moved, every client with a
// cached key set would start rejecting perfectly good tokens.
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
)

// MinKeySizeBits is the smallest RSA modulus accepted. Below 2048 the signature
// is not worth the bytes it takes up.
const MinKeySizeBits = 2048

// PrivateKey is a signing key together with the id published for it.
type PrivateKey struct {
	// ID is the `kid`. It must be stable for the life of the key: it is how a
	// verifier finds the right public key for a token.
	ID  string
	Key *rsa.PrivateKey
}

// ParsePrivateKeyPEM reads a PEM-encoded RSA private key in either PKCS#1 or
// PKCS#8 form, since both are what `openssl genpkey` and `openssl genrsa`
// produce and neither is worth making the operator care about.
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
		// The error can quote key bytes, so it is not wrapped.
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

// KeySet is every key a verifier will accept, plus which one currently signs.
type KeySet struct {
	active  PrivateKey
	byKeyID map[string]*rsa.PublicKey
}

// NewKeySet builds a key set from the signing key and any additional keys that
// must stay verifiable — the previous key during a rotation, typically.
//
// The active key is always part of the published set; passing it again in
// additional is harmless.
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

// Active returns the key new tokens are signed with.
func (s *KeySet) Active() PrivateKey { return s.active }

// PublicKey returns the public key published under keyID.
func (s *KeySet) PublicKey(keyID string) (*rsa.PublicKey, bool) {
	key, ok := s.byKeyID[keyID]
	return key, ok
}

// KeyIDs returns every published key id, sorted, so callers and tests get a
// stable order out of the underlying map.
func (s *KeySet) KeyIDs() []string {
	ids := make([]string, 0, len(s.byKeyID))
	for id := range s.byKeyID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// jwk is one entry of a JWKS document, per RFC 7517.
type jwk struct {
	KeyType   string `json:"kty"`
	Use       string `json:"use"`
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	// Modulus and Exponent are base64url without padding, per RFC 7518.
	Modulus  string `json:"n"`
	Exponent string `json:"e"`
}

// jwks is the document served at the JWKS endpoint.
type jwks struct {
	Keys []jwk `json:"keys"`
}

// JWKS renders the public half of the key set as an RFC 7517 document. This is
// what every other service fetches and caches; nothing private is in it.
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

// bigEndian renders the public exponent as the shortest big-endian byte string,
// which is how RFC 7518 wants it — 65537 becomes AQAB, not AAEAAQ.
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
