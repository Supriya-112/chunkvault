package vault

import (
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// Sentinel errors so callers (and the CLI) can react to encryption state
// without string-matching.
var (
	// ErrPassphraseRequired is returned when opening an encrypted vault without
	// a passphrase.
	ErrPassphraseRequired = errors.New("vault is encrypted: a passphrase is required")
	// ErrNotEncrypted is returned when a passphrase is supplied for a vault that
	// is not encrypted.
	ErrNotEncrypted = errors.New("vault is not encrypted: no passphrase expected")
	// ErrWrongPassphrase is returned when the supplied passphrase does not match
	// the one the vault was created with.
	ErrWrongPassphrase = errors.New("wrong passphrase")
)

// configName is the vault metadata file. Its presence with a KDF section marks
// a vault as encrypted.
const configName = "config.json"

// verifierPlaintext is sealed at vault creation and re-opened on every unlock:
// if it decrypts back to this value, the passphrase is correct.
var verifierPlaintext = []byte("chunkvault-verifier-v1")

// Argon2id parameters. They are stored in the vault config, so changing these
// defaults does not break vaults created with the old values.
const (
	argonTime    = 2
	argonMemory  = 32 * 1024 // KiB, i.e. 32 MiB
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

// kdfParams records how a passphrase is stretched into the master key. Every
// field is persisted so a vault stays openable if the defaults above change.
type kdfParams struct {
	Algo    string `json:"algo"` // always "argon2id" for now
	Salt    []byte `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
	KeyLen  uint32 `json:"keyLen"`
}

// vaultConfig is the on-disk <vault>/config.json. A nil KDF means the vault is
// not encrypted.
type vaultConfig struct {
	Version  int        `json:"version"`
	KDF      *kdfParams `json:"kdf,omitempty"`
	Verifier []byte     `json:"verifier,omitempty"` // verifierPlaintext sealed under the master key
}

// encryptor holds the keys derived from a passphrase: an AEAD for chunk and
// manifest contents, and a separate MAC key for naming chunks.
type encryptor struct {
	aead    cipher.AEAD // XChaCha20-Poly1305
	nameKey []byte      // HMAC key for content-addressing without leaking plaintext hashes
}

// deriveMaster stretches a passphrase into the 32-byte master key.
func deriveMaster(passphrase []byte, p *kdfParams) []byte {
	return argon2.IDKey(passphrase, p.Salt, p.Time, p.Memory, p.Threads, p.KeyLen)
}

// newEncryptor splits the master key into an encryption subkey and a naming
// subkey (domain-separated via HKDF) and builds the AEAD.
func newEncryptor(master []byte) (*encryptor, error) {
	encKey := hkdfExpand(master, "chunkvault:aead:v1", chacha20poly1305.KeySize)
	nameKey := hkdfExpand(master, "chunkvault:name:v1", 32)
	aead, err := chacha20poly1305.NewX(encKey)
	if err != nil {
		return nil, err
	}
	return &encryptor{aead: aead, nameKey: nameKey}, nil
}

// hkdfExpand derives n bytes of subkey from the master key under a label.
func hkdfExpand(master []byte, info string, n int) []byte {
	r := hkdf.New(sha256.New, master, nil, []byte(info))
	out := make([]byte, n)
	if _, err := io.ReadFull(r, out); err != nil {
		panic("hkdf: " + err.Error()) // reading from HKDF cannot fail for sane sizes
	}
	return out
}

// seal encrypts plaintext, returning nonce || ciphertext. A fresh random nonce
// is used per call; XChaCha20's 24-byte nonce makes random nonces safe at the
// scale a vault reaches.
func (e *encryptor) seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	// Seal appends to nonce, so the result is nonce followed by the ciphertext.
	return e.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// open reverses seal.
func (e *encryptor) open(blob []byte) ([]byte, error) {
	ns := e.aead.NonceSize()
	if len(blob) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return e.aead.Open(nil, blob[:ns], blob[ns:], nil)
}

// chunkID is the content address of a chunk under encryption: an HMAC of the
// plaintext keyed by nameKey, so the on-disk name reveals nothing about the
// contents while still being deterministic (identical chunks still dedup).
func (e *encryptor) chunkID(data []byte) string {
	mac := hmac.New(sha256.New, e.nameKey)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// newEncryptedConfig creates the config for a brand-new encrypted vault along
// with the encryptor that matches it.
func newEncryptedConfig(passphrase []byte) (*vaultConfig, *encryptor, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, err
	}
	p := &kdfParams{Algo: "argon2id", Salt: salt, Time: argonTime, Memory: argonMemory, Threads: argonThreads, KeyLen: argonKeyLen}
	enc, err := newEncryptor(deriveMaster(passphrase, p))
	if err != nil {
		return nil, nil, err
	}
	verifier, err := enc.seal(verifierPlaintext)
	if err != nil {
		return nil, nil, err
	}
	return &vaultConfig{Version: 1, KDF: p, Verifier: verifier}, enc, nil
}

// openEncryptor rebuilds the encryptor for an existing encrypted vault and
// confirms the passphrase against the stored verifier.
func openEncryptor(cfg *vaultConfig, passphrase []byte) (*encryptor, error) {
	enc, err := newEncryptor(deriveMaster(passphrase, cfg.KDF))
	if err != nil {
		return nil, err
	}
	got, err := enc.open(cfg.Verifier)
	if err != nil || subtle.ConstantTimeCompare(got, verifierPlaintext) != 1 {
		return nil, ErrWrongPassphrase
	}
	return enc, nil
}

// loadConfig reads a vault's config, returning (nil, nil) when the vault has
// none (an unencrypted vault, including those from before M10).
func loadConfig(be backend) (*vaultConfig, error) {
	data, err := be.get(configName)
	if errors.Is(err, errNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg vaultConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("reading vault config: %w", err)
	}
	return &cfg, nil
}

// saveConfig writes a vault's config.
func saveConfig(be backend, cfg *vaultConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return be.put(configName, data)
}

// IsEncrypted reports whether the vault at location is encrypted, so a caller
// can decide up front whether it needs to collect a passphrase. A location that
// does not exist yet is reported as not encrypted (no error).
func IsEncrypted(location string) (bool, error) {
	be, err := newBackend(location)
	if err != nil {
		return false, err
	}
	cfg, err := loadConfig(be)
	if err != nil {
		return false, err
	}
	return cfg != nil && cfg.KDF != nil, nil
}
