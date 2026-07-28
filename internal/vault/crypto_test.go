package vault

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

// fixedKey returns a deterministic 32-byte master key so unit tests avoid the
// (deliberately slow) Argon2id step.
func fixedKey(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func TestSealOpenRoundTrip(t *testing.T) {
	enc, err := newEncryptor(fixedKey(0x2a))
	if err != nil {
		t.Fatalf("newEncryptor: %v", err)
	}
	msg := []byte("attack at dawn")
	blob, err := enc.seal(msg)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(blob, msg) {
		t.Fatal("sealed output still contains the plaintext")
	}
	got, err := enc.open(blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}

func TestOpenRejectsTampering(t *testing.T) {
	enc, _ := newEncryptor(fixedKey(7))
	blob, _ := enc.seal([]byte("secret"))
	blob[len(blob)-1] ^= 0xff // corrupt the authentication tag
	if _, err := enc.open(blob); err == nil {
		t.Fatal("open should reject a tampered ciphertext")
	}
	if _, err := enc.open([]byte{1, 2, 3}); err == nil {
		t.Fatal("open should reject a too-short blob")
	}
}

func TestOpenWithWrongKeyFails(t *testing.T) {
	a, _ := newEncryptor(fixedKey(1))
	b, _ := newEncryptor(fixedKey(2))
	blob, _ := a.seal([]byte("secret"))
	if _, err := b.open(blob); err == nil {
		t.Fatal("open with a different key should fail")
	}
}

func TestChunkIDIsKeyedAndDeterministic(t *testing.T) {
	a, _ := newEncryptor(fixedKey(1))
	b, _ := newEncryptor(fixedKey(2))
	data := []byte("chunk contents")

	if a.chunkID(data) != a.chunkID(data) {
		t.Fatal("chunkID must be deterministic for the same key")
	}
	if a.chunkID(data) == b.chunkID(data) {
		t.Fatal("chunkID must differ under different keys")
	}
	sum := sha256.Sum256(data)
	if a.chunkID(data) == hex.EncodeToString(sum[:]) {
		t.Fatal("encrypted chunkID must not equal the plaintext SHA-256")
	}
}

func TestPassphraseVerifier(t *testing.T) {
	cfg, _, err := newEncryptedConfig([]byte("hunter2"))
	if err != nil {
		t.Fatalf("newEncryptedConfig: %v", err)
	}
	if _, err := openEncryptor(cfg, []byte("hunter2")); err != nil {
		t.Fatalf("correct passphrase rejected: %v", err)
	}
	if _, err := openEncryptor(cfg, []byte("wrong")); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong passphrase: got %v, want ErrWrongPassphrase", err)
	}
}
