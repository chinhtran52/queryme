package secret

import (
	"bytes"
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestEncryptRoundTripIsBoundToContext(t *testing.T) {
	c, err := NewCipher(newKey())
	if err != nil {
		t.Fatal(err)
	}
	enc, err := c.Encrypt("s3cret", "postgres://a@h/db")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.Decrypt(enc, "postgres://a@h/db"); err != nil || got != "s3cret" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := c.Decrypt(enc, "postgres://other@h/db"); err == nil {
		t.Fatal("ciphertext must not decrypt under another URL")
	}
	other, _ := NewCipher(newKey())
	if _, err := other.Decrypt(enc, "postgres://a@h/db"); err == nil {
		t.Fatal("ciphertext must not decrypt with another key")
	}
}

func TestLoadKeyUsesKeyringAndReusesIt(t *testing.T) {
	keyring.MockInit()
	first, err := LoadKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadKey()
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("key changed between loads: %v", err)
	}
}

func TestLoadKeyFailsWithoutKeyring(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	if key, err := LoadKey(); err == nil || key != nil {
		t.Fatalf("got key=%v err=%v, want an error", key, err)
	}
}
