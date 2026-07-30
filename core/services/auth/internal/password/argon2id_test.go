package password_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/auth/internal/password"
)

// testParams keeps the memory cost low so the suite does not spend seconds
// proving what it could prove in milliseconds. Correctness does not depend on
// the cost; only the price of an offline attack does.
func testParams() password.Params {
	p := password.DefaultParams()
	p.Memory = 8 * 1024
	p.Time = 1
	return p
}

func newHasher(t *testing.T) *password.Hasher {
	t.Helper()

	h, err := password.NewHasher(testParams())
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	return h
}

func TestHashThenVerify(t *testing.T) {
	h := newHasher(t)

	encoded, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	ok, err := h.Verify("correct horse battery staple", encoded)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("the password that produced the hash did not verify against it")
	}
}

func TestWrongPasswordDoesNotVerify(t *testing.T) {
	h := newHasher(t)

	encoded, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	for _, wrong := range []string{
		"correct horse battery stapl",
		"correct horse battery staple ",
		"Correct horse battery staple",
		"",
	} {
		ok, err := h.Verify(wrong, encoded)
		if err != nil {
			t.Fatalf("Verify(%q): %v", wrong, err)
		}
		if ok {
			t.Errorf("wrong password %q verified", wrong)
		}
	}
}

// A leaked table must not reveal which accounts share a password, which is what
// a per-hash random salt buys.
func TestSamePasswordHashesDifferently(t *testing.T) {
	h := newHasher(t)

	const pw = "shared between two accounts"

	first, err := h.Hash(pw)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	second, err := h.Hash(pw)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if first == second {
		t.Fatal("two hashes of the same password are identical; the salt is not random")
	}

	// Both still have to verify — different, not broken.
	for i, encoded := range []string{first, second} {
		ok, err := h.Verify(pw, encoded)
		if err != nil {
			t.Fatalf("Verify(%d): %v", i, err)
		}
		if !ok {
			t.Fatalf("hash %d did not verify", i)
		}
	}
}

func TestEncodedFormat(t *testing.T) {
	h := newHasher(t)

	encoded, err := h.Hash("whatever")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Fatalf("encoded hash does not start with the PHC prefix: %q", encoded)
	}

	// $ + argon2id + v + params + salt + key
	if got := len(strings.Split(encoded, "$")); got != 6 {
		t.Fatalf("encoded hash has %d fields, want 6: %q", got, encoded)
	}

	// The parameters have to travel with the hash, otherwise the cost can never
	// be raised without invalidating every stored password.
	if !strings.Contains(encoded, "m=8192,t=1,p=") {
		t.Fatalf("parameters are missing from the encoded hash: %q", encoded)
	}
}

// A hash written under an older, cheaper configuration must keep verifying;
// otherwise raising the cost would sign everybody out.
func TestVerifyUsesTheStoredParameters(t *testing.T) {
	weak := testParams()
	weak.Memory = 8 * 1024
	weak.Time = 1

	oldHasher, err := password.NewHasher(weak)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}

	encoded, err := oldHasher.Hash("legacy password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	strong := weak
	strong.Memory = 32 * 1024
	strong.Time = 3

	newHasher, err := password.NewHasher(strong)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}

	ok, err := newHasher.Verify("legacy password", encoded)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("a hash created with weaker parameters stopped verifying after the cost was raised")
	}
	if !newHasher.NeedsRehash(encoded) {
		t.Error("NeedsRehash did not flag a hash created with weaker parameters")
	}
}

func TestNeedsRehash(t *testing.T) {
	h := newHasher(t)

	current, err := h.Hash("current")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h.NeedsRehash(current) {
		t.Error("a hash created with the current parameters was flagged for rehash")
	}

	// Unparseable is not "outdated" — that account needs a reset, and saying
	// otherwise sends the caller down the wrong path.
	if h.NeedsRehash("not a hash at all") {
		t.Error("a malformed hash was reported as merely needing a rehash")
	}
}

// A wrong password is a false result; only an unreadable stored hash is an
// error. Conflating the two would report a corrupted column as a bad password.
func TestMalformedHashIsAnErrorNotAMismatch(t *testing.T) {
	h := newHasher(t)

	valid, err := h.Hash("password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	parts := strings.Split(valid, "$")

	tests := map[string]string{
		"empty":              "",
		"not phc":            "plaintext",
		"too few fields":     "$argon2id$v=19$m=8192,t=1,p=1$c2FsdA",
		"wrong algorithm":    "$bcrypt$v=19$m=8192,t=1,p=1$" + parts[4] + "$" + parts[5],
		"unknown version":    "$argon2id$v=13$m=8192,t=1,p=1$" + parts[4] + "$" + parts[5],
		"unparsable params":  "$argon2id$v=19$m=lots,t=1,p=1$" + parts[4] + "$" + parts[5],
		"salt not base64":    "$argon2id$v=19$m=8192,t=1,p=1$!!!!$" + parts[5],
		"key not base64":     "$argon2id$v=19$m=8192,t=1,p=1$" + parts[4] + "$!!!!",
		"empty salt and key": "$argon2id$v=19$m=8192,t=1,p=1$$",
	}

	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			ok, err := h.Verify("password", encoded)

			if ok {
				t.Fatal("a malformed hash verified")
			}
			if !errors.Is(err, password.ErrMalformedHash) {
				t.Fatalf("error = %v, want ErrMalformedHash", err)
			}
		})
	}
}

func TestParamsValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*password.Params)
		wantErr bool
	}{
		{name: "defaults are valid", mutate: func(*password.Params) {}},
		{name: "memory too low", mutate: func(p *password.Params) { p.Memory = 1024 }, wantErr: true},
		{name: "no passes", mutate: func(p *password.Params) { p.Time = 0 }, wantErr: true},
		{name: "no lanes", mutate: func(p *password.Params) { p.Parallelism = 0 }, wantErr: true},
		{name: "salt too short", mutate: func(p *password.Params) { p.SaltLength = 8 }, wantErr: true},
		{name: "key too short", mutate: func(p *password.Params) { p.KeyLength = 8 }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := password.DefaultParams()
			tt.mutate(&p)

			err := p.Validate()

			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			// A bad configuration has to fail at construction, not at the first
			// registration.
			if _, err := password.NewHasher(p); (err != nil) != tt.wantErr {
				t.Fatalf("NewHasher() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultParams(t *testing.T) {
	p := password.DefaultParams()

	if err := p.Validate(); err != nil {
		t.Fatalf("the defaults do not pass their own validation: %v", err)
	}
	// RFC 9106's second recommended option.
	if p.Memory != 64*1024 {
		t.Errorf("Memory = %d KiB, want 65536", p.Memory)
	}
	if p.Time != 3 {
		t.Errorf("Time = %d, want 3", p.Time)
	}
	if p.Parallelism < 1 || p.Parallelism > 4 {
		t.Errorf("Parallelism = %d, want between 1 and 4", p.Parallelism)
	}
}

// Long passwords are allowed up to the domain limit, and argon2id has no silent
// truncation the way bcrypt does at 72 bytes.
func TestLongPasswordIsNotTruncated(t *testing.T) {
	h := newHasher(t)

	base := strings.Repeat("a", 80)

	encoded, err := h.Hash(base + "X")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	ok, err := h.Verify(base+"Y", encoded)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Fatal("two 81-byte passwords differing in the last byte verified against each other")
	}
}
