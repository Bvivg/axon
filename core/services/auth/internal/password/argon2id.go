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

type Params struct {
	Memory uint32

	Time uint32

	Parallelism uint8

	SaltLength uint32

	KeyLength uint32
}

func DefaultParams() Params {
	return Params{
		Memory:      64 * 1024,
		Time:        3,
		Parallelism: lanes(),
		SaltLength:  16,
		KeyLength:   32,
	}
}

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

type Hasher struct {
	params Params
}

func NewHasher(params Params) (*Hasher, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}
	return &Hasher{params: params}, nil
}

var ErrMalformedHash = errors.New("password: malformed hash")

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

func (h *Hasher) Verify(password, encoded string) (bool, error) {
	params, salt, want, err := decode(encoded)
	if err != nil {
		return false, err
	}

	got := argon2.IDKey(
		[]byte(password),
		salt,
		params.Time,
		params.Memory,
		params.Parallelism,
		uint32(len(want)),
	)

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func (h *Hasher) NeedsRehash(encoded string) bool {
	params, _, key, err := decode(encoded)
	if err != nil {

		return false
	}

	return params.Memory < h.params.Memory ||
		params.Time < h.params.Time ||
		uint32(len(key)) < h.params.KeyLength
}

func (h *Hasher) Params() Params { return h.params }

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

func decode(encoded string) (Params, []byte, []byte, error) {

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
