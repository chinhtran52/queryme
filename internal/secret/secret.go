package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "queryme"
	keyringUser    = "history-key"
	keySize        = 32
)

// Cipher encrypts saved passwords with AES-256-GCM.
type Cipher struct{ aead cipher.AEAD }

func NewCipher(key []byte) (*Cipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt binds the ciphertext to context (the connection URL), so it cannot be moved to another entry.
func (c *Cipher) Encrypt(plain, context string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plain), []byte(context))
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *Cipher) Decrypt(encoded, context string) (string, error) {
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	size := c.aead.NonceSize()
	if len(sealed) < size {
		return "", errors.New("ciphertext too short")
	}
	plain, err := c.aead.Open(nil, sealed[:size], sealed[size:], []byte(context))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// LoadKey returns the encryption key from the OS keyring, creating it on first use.
// It fails when no keyring is reachable; callers then do not save passwords at all.
func LoadKey() ([]byte, error) {
	stored, err := keyring.Get(keyringService, keyringUser)
	if err == nil {
		return decodeKey(stored)
	}
	if !errors.Is(err, keyring.ErrNotFound) {
		return nil, fmt.Errorf("OS keyring unavailable: %w", err)
	}
	key := newKey()
	if err := keyring.Set(keyringService, keyringUser, base64.StdEncoding.EncodeToString(key)); err != nil {
		return nil, fmt.Errorf("OS keyring unavailable: %w", err)
	}
	return key, nil
}

func newKey() []byte {
	key := make([]byte, keySize)
	_, _ = rand.Read(key) // crypto/rand.Read never fails on supported platforms.
	return key
}

func decodeKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err == nil && len(key) != keySize {
		err = fmt.Errorf("key is %d bytes, want %d", len(key), keySize)
	}
	return key, err
}
