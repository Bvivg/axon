package keyring

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

const EncryptionKeySize = 32

var ErrSealed = errors.New("keyring: sealed key cannot be opened")

type Sealer struct {
	aead cipher.AEAD
}

func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != EncryptionKeySize {
		return nil, fmt.Errorf("keyring: encryption key is %d bytes, want %d", len(key), EncryptionKeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("keyring: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("keyring: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

func (s *Sealer) Seal(plaintext []byte, keyID string) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize(), s.aead.NonceSize()+len(plaintext)+s.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("keyring: nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, []byte(keyID)), nil
}

func (s *Sealer) Open(sealed []byte, keyID string) ([]byte, error) {
	size := s.aead.NonceSize()
	if len(sealed) < size+s.aead.Overhead() {
		return nil, ErrSealed
	}
	plaintext, err := s.aead.Open(nil, sealed[:size], sealed[size:], []byte(keyID))
	if err != nil {
		return nil, ErrSealed
	}
	return plaintext, nil
}
