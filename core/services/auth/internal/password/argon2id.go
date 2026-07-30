// Package password hashes and verifies passwords with argon2id.
//
// argon2id rather than bcrypt: it is memory-hard, which is what makes a GPU or
// ASIC farm a poor investment against it, and it has no silent input-length
// limit to design around.
//
// The encoded hash carries its own parameters in the standard PHC string format,
// so raising the cost later is a matter of changing the configuration. Existing
// passwords keep verifying against the parameters they were created with, and
// NeedsRehash reports which ones are behind, so an upgrade happens gradually as
// people sign in instead of locking everybody out at once.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params configures the cost of hashing.
type Params struct {
	// Memory is the memory cost in KiB.
	Memory uint32
	// Time is the number of passes over memory.
	Time uint32
	// Parallelism is the number of lanes.
	Parallelism uint8
	// SaltLength is the salt size in bytes.
	SaltLength uint32
	// KeyLength is the derived key size in bytes.
	KeyLength uint32
}

// DefaultParams follows the RFC 9106 second recommended option: 64 MiB of
// memory with three passes. It is a deliberate middle ground — enough to make
// offline cracking expensive, cheap enough that a login is not a visible pause
// and a burst of sign-ins cannot exhaust the service's memory.
func DefaultParams() Params {
	return Params{
		Memory:      64 * 1024,
		Time:        3,
		Parallelism: lanes(),
		SaltLength:  16,
		KeyLength:   32,
	}
}

// lanes picks the parallelism from the available CPUs, capped at 4. Beyond that
// the gain flattens while memory pressure per concurrent login keeps growing.
func lanes() uint8 {
	n := runtime.NumCPU()
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return uint8(n)
}

// Validate reports whether the parameters are usable. It exists so a bad
// configuration fails at startup rather than at the first registration.
func (p Params) Validate() error {
	switch {
	case p.Memory < 8*1024:
		return fmt.Errorf("password: memory cost %d KiB is too low, want at least 8192", p.Memory)
	case p.Time < 1:
		return errors.New("password: time cost must be at least 1")
	case p.Parallelism < 1:
		return errors.New("password: parallelism must be at least 1")
	case p.SaltLength < 16:
		return fmt.Errorf("password: salt length %d is too short, want at least 16", p.SaltLength)
	case p.KeyLength < 16:
		return fmt.Errorf("password: key length %d is too short, want at least 16", p.KeyLength)
	}
	return nil
}

// Hasher hashes and verifies passwords.
type Hasher struct {
	params Params
}

// NewHasher returns a Hasher using params.
func NewHasher(params Params) (*Hasher, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}
	return &Hasher{params: params}, nil
}

// ErrMalformedHash is returned when a stored hash cannot be parsed. In practice
// it means the column was corrupted or written by something else.
var ErrMalformedHash = errors.New("password: malformed hash")

// Hash derives an encoded hash for password.
//
// Two calls with the same password return different strings, because each gets a
// fresh random salt. That is what stops a leaked table from revealing which
// accounts share a password.
func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: read salt: %w", err)
	}

	key := argon2.IDKey(
		[]byte(password),
		salt,
		h.params.Time,
		h.params.Memory,
		h.params.Parallelism,
		h.params.KeyLength,
	)

	return encode(h.params, salt, key), nil
}

// Verify reports whether password matches encoded.
//
// A wrong password is not an error: it is a false result. An error means the
// stored hash could not be parsed, which is an operational problem and must not
// be reported to the caller as "wrong password".
func (h *Hasher) Verify(password, encoded string) (bool, error) {
	params, salt, want, err := decode(encoded)
	if err != nil {
		return false, err
	}

	// Derived with the stored parameters, not the current ones, so a hash written
	// under an older cost still verifies.
	got := argon2.IDKey(
		[]byte(password),
		salt,
		params.Time,
		params.Memory,
		params.Parallelism,
		uint32(len(want)),
	)

	// Constant time: a byte-by-byte comparison that returns early leaks how much
	// of the hash matched, one request at a time.
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// NeedsRehash reports whether encoded was produced with weaker parameters than
// the ones currently configured. Callers check it after a successful sign-in and
// re-hash in place, which upgrades accounts as their owners appear.
func (h *Hasher) NeedsRehash(encoded string) bool {
	params, _, key, err := decode(encoded)
	if err != nil {
		// Unparseable is not "outdated" — it needs a password reset, and saying
		// otherwise here would send the caller down the wrong path.
		return false
	}

	return params.Memory < h.params.Memory ||
		params.Time < h.params.Time ||
		uint32(len(key)) < h.params.KeyLength
}

// Params returns the parameters new hashes are created with.
func (h *Hasher) Params() Params { return h.params }

// encode renders the PHC string:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt>$<key>
//
// Base64 is the raw, unpadded variant the format specifies.
func encode(p Params, salt, key []byte) string {
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		p.Memory,
		p.Time,
		p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

// decode parses a PHC string back into its parts.
func decode(encoded string) (Params, []byte, []byte, error) {
	// Leading empty field from the leading '$'.
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" {
		return Params{}, nil, nil, ErrMalformedHash
	}

	if parts[1] != "argon2id" {
		return Params{}, nil, nil, fmt.Errorf("%w: algorithm %q is not argon2id", ErrMalformedHash, parts[1])
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}
	if version != argon2.Version {
		return Params{}, nil, nil, fmt.Errorf("%w: version %d is unsupported", ErrMalformedHash, version)
	}

	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Parallelism); err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}

	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}

	if len(salt) == 0 || len(key) == 0 {
		return Params{}, nil, nil, ErrMalformedHash
	}

	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(key))

	return p, salt, key, nil
}
