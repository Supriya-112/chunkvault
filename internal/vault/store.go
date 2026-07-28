// Package vault implements the on-disk chunk store, snapshot format, and the
// backup operation that ties them together.
//
// Layout on disk:
//
//	<vault>/
//	  config.json                 present only for encrypted vaults (KDF + verifier)
//	  chunks/<aa>/<full-id>        one file per unique chunk, named by its ID
//	  snapshots/<id>.json          a manifest of files and their chunk IDs
//
// A chunk's ID is the SHA-256 of its contents for an unencrypted vault, or an
// HMAC of its contents (keyed by a passphrase-derived key) for an encrypted
// one, so encrypted vaults never expose plaintext hashes as file names.
package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store is a content-addressable chunk store rooted at a directory. It is safe
// for concurrent use by multiple goroutines.
type Store struct {
	root  string
	codec Codec      // compression applied to newly written chunks; reads honour each chunk's own tag
	enc   *encryptor // non-nil for an encrypted vault; encrypts chunks and manifests and keys chunk IDs

	mu   sync.Mutex
	seen map[string]bool // chunk IDs known to be stored, so concurrent PutChunk calls write each chunk once
}

// Open opens (creating if needed) an unencrypted vault at root. It is a thin
// wrapper over openStore for callers that never use encryption.
func Open(root string) (*Store, error) {
	return openStore(root, nil, true)
}

// openStore opens a vault at root, creating it when create is true. Encryption
// is resolved from the vault's config and the supplied passphrase:
//
//   - encrypted vault  → a correct passphrase is required (else an error)
//   - unencrypted vault → no passphrase is expected
//   - brand-new vault + passphrase → created as an encrypted vault
//
// A passphrase given for an established unencrypted vault is refused rather
// than silently ignored.
func openStore(root string, passphrase []byte, create bool) (*Store, error) {
	// When not creating, the vault root must already exist, so a mistyped path
	// is reported instead of silently treated as an empty vault.
	if !create {
		if _, err := os.Stat(root); err != nil {
			return nil, fmt.Errorf("vault %q: %w", root, err)
		}
	}
	for _, sub := range []string{"chunks", "snapshots"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", sub, err)
		}
	}

	cfg, err := loadConfig(root)
	if err != nil {
		return nil, err
	}
	s := &Store{root: root, seen: map[string]bool{}}

	switch {
	case cfg != nil && cfg.KDF != nil:
		if len(passphrase) == 0 {
			return nil, ErrPassphraseRequired
		}
		if s.enc, err = openEncryptor(cfg, passphrase); err != nil {
			return nil, err
		}
	case len(passphrase) != 0:
		// The vault has no encryption. Turning a brand-new vault into an
		// encrypted one is fine; adding a passphrase to one that already holds
		// data is not.
		if !create || established(root) {
			return nil, ErrNotEncrypted
		}
		newCfg, enc, err := newEncryptedConfig(passphrase)
		if err != nil {
			return nil, err
		}
		if err := saveConfig(root, newCfg); err != nil {
			return nil, err
		}
		s.enc = enc
	}
	return s, nil
}

// established reports whether a vault already holds at least one snapshot, so a
// passphrase cannot be used to "encrypt" a vault that already has plaintext
// data in it.
func established(root string) bool {
	entries, err := os.ReadDir(filepath.Join(root, "snapshots"))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			return true
		}
	}
	return false
}

// chunkPath returns the on-disk path for a chunk, sharding by the first two
// characters of its ID to avoid huge single directories.
func (s *Store) chunkPath(id string) string {
	return filepath.Join(s.root, "chunks", id[:2], id)
}

// chunkID is the content address of data: an HMAC keyed by the vault's naming
// key when encrypted, otherwise the SHA-256 of the contents. Either way it is
// deterministic, so identical chunks still deduplicate.
func (s *Store) chunkID(data []byte) string {
	if s.enc != nil {
		return s.enc.chunkID(data)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// PutChunk stores data under its content ID and returns that ID. The ID is
// taken over the uncompressed plaintext (see chunkID), so deduplication is
// unaffected by compression or encryption. wasNew is false when the chunk
// already existed — a deduplication hit — and stored is the number of bytes
// actually written to disk (0 on a dedup hit). The write is atomic: content
// goes to a temp file that is renamed into place.
//
// Each chunk is compressed and then, for an encrypted vault, encrypted before
// it is written. It is safe to call concurrently: a chunk is claimed under a
// lock before the write, so exactly one caller writes any given chunk.
func (s *Store) PutChunk(data []byte) (id string, wasNew bool, stored int, err error) {
	id = s.chunkID(data)
	dst := s.chunkPath(id)

	s.mu.Lock()
	if s.seen[id] {
		s.mu.Unlock()
		return id, false, 0, nil // another chunk with this content already handled it
	}
	if _, statErr := os.Stat(dst); statErr == nil {
		s.seen[id] = true
		s.mu.Unlock()
		return id, false, 0, nil // already on disk from a previous run — dedup
	}
	s.seen[id] = true // claim it; we are the one writer
	s.mu.Unlock()

	// The write happens outside the lock so chunks store in parallel. On
	// failure the claim is released so a retry can store the chunk.
	defer func() {
		if err != nil {
			s.mu.Lock()
			delete(s.seen, id)
			s.mu.Unlock()
		}
	}()

	blob, err := encodeChunk(s.codec, data)
	if err != nil {
		return "", false, 0, err
	}
	if s.enc != nil {
		if blob, err = s.enc.seal(blob); err != nil {
			return "", false, 0, err
		}
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", false, 0, err
	}
	tmp := dst + ".tmp"
	if err = os.WriteFile(tmp, blob, 0o644); err != nil {
		return "", false, 0, err
	}
	if err = os.Rename(tmp, dst); err != nil {
		return "", false, 0, err
	}
	return id, true, len(blob), nil
}

// GetChunk returns the original contents of the chunk with the given ID,
// decrypting it (for an encrypted vault) and decompressing it. It verifies that
// the recovered bytes reproduce the ID the chunk is stored under, so a
// corrupted, tampered, or undecodable chunk is reported as an integrity failure
// rather than restored.
func (s *Store) GetChunk(id string) ([]byte, error) {
	raw, err := os.ReadFile(s.chunkPath(id))
	if err != nil {
		return nil, err
	}
	blob := raw
	if s.enc != nil {
		if blob, err = s.enc.open(raw); err != nil {
			return nil, fmt.Errorf("chunk %s failed integrity check: %w", id, err)
		}
	}
	data, err := decodeChunk(blob)
	if err != nil {
		return nil, fmt.Errorf("chunk %s failed integrity check: %w", id, err)
	}
	if s.chunkID(data) != id {
		return nil, fmt.Errorf("chunk %s failed integrity check: content id mismatch", id)
	}
	return data, nil
}

// ListSnapshots returns the IDs of every snapshot in the vault, sorted.
func (s *Store) ListSnapshots() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "snapshots"))
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}
	return ids, nil
}
