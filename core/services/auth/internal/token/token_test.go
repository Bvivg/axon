package token_test

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/bvivg/axon/core/services/auth/internal/token"
)

func TestNewProducesUniqueValues(t *testing.T) {
	seen := make(map[string]struct{}, 2000)

	for range 1000 {
		tok, err := token.New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		if _, dup := seen[tok.Value]; dup {
			t.Fatal("New returned a duplicate token value")
		}
		if _, dup := seen[tok.Hash]; dup {
			t.Fatal("New returned a duplicate token hash")
		}
		seen[tok.Value] = struct{}{}
		seen[tok.Hash] = struct{}{}
	}
}

func TestNewHasFullEntropy(t *testing.T) {
	tok, err := token.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	raw, err := base64.RawURLEncoding.DecodeString(tok.Value)
	if err != nil {
		t.Fatalf("token value is not raw base64url: %v", err)
	}
	if len(raw) != token.Bytes {
		t.Fatalf("token carries %d bytes of entropy, want %d", len(raw), token.Bytes)
	}
}

func TestHashMatchesTheGeneratedHash(t *testing.T) {
	tok, err := token.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := token.Hash(tok.Value)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if got != tok.Hash {
		t.Fatalf("Hash(value) = %q, want the hash New returned (%q)", got, tok.Hash)
	}
}

func TestHashDoesNotContainTheValue(t *testing.T) {
	tok, err := token.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if tok.Hash == tok.Value {
		t.Fatal("the stored hash is the token value itself")
	}
}

func TestHashIsDeterministic(t *testing.T) {
	first, err := token.Hash("some-token-value")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	second, err := token.Hash("some-token-value")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if first != second {
		t.Fatal("Hash is not deterministic; a stored token could never be found again")
	}
}

func TestHashRejectsEmpty(t *testing.T) {
	_, err := token.Hash("")

	if !errors.Is(err, token.ErrEmpty) {
		t.Fatalf("Hash(\"\") error = %v, want ErrEmpty", err)
	}
}

func TestEqual(t *testing.T) {
	tok, err := token.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !token.Equal(tok.Hash, tok.Hash) {
		t.Error("Equal reported identical hashes as different")
	}

	other, err := token.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if token.Equal(tok.Hash, other.Hash) {
		t.Error("Equal reported different hashes as identical")
	}

	if token.Equal(tok.Hash, tok.Hash[:len(tok.Hash)-1]) {
		t.Error("Equal reported a truncated hash as identical")
	}
	if token.Equal("", "") == false {
		t.Error("Equal on two empty strings should be true; it is the caller's job to reject empties")
	}
}
