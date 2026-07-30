// Package vault implements the chunk store, snapshot format, and the backup
// operation that ties them together.
//
// Storage sits behind a backend (see backend.go): a flat namespace of keys that
// is served by either the local filesystem or S3. The vault layout is:
//
//	config.json                 present only for encrypted vaults (KDF + verifier)
//	chunks/<aa>/<full-id>        one object per unique chunk, named by its ID
//	snapshots/<id>.json          a manifest of files and their chunk IDs
//
// A chunk's ID is the SHA-256 of its contents for an unencrypted vault, or an
// HMAC of its contents (keyed by a passphrase-derived key) for an encrypted
// one, so encrypted vaults never expose plaintext hashes as key names.
package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Store is a content-addressable chunk store over a backend. It is safe for
// concurrent use by multiple goroutines.
type Store struct {
	backend backend
	codec   Codec      // compression applied to newly written chunks; reads honour each chunk's own tag
	enc     *encryptor // non-nil for an encrypted vault; encrypts chunks and manifests and keys chunk IDs

	mu   sync.Mutex
	seen map[string]bool // chunk IDs claimed for writing, so concurrent PutChunk calls write each chunk once
}

// Open opens (creating if needed) an unencrypted vault at location. It is a thin
// wrapper over openStore for callers that never use encryption.
func Open(location string) (*Store, error) {
	return openStore(location, nil, true)
}

// openStore opens a vault at location, creating it when create is true.
// Encryption is resolved from the vault's config and the supplied passphrase:
//
//   - encrypted vault  → a correct passphrase is required (else an error)
//   - unencrypted vault → no passphrase is expected
//   - brand-new vault + passphrase → created as an encrypted vault
//
// A passphrase given for an established unencrypted vault is refused rather
// than silently ignored.
func openStore(location string, passphrase []byte, create bool) (*Store, error) {
	be, err := newBackend(location)
	if err != nil {
		return nil, err
	}
	if err := be.open(create); err != nil {
		return nil, err
	}

	cfg, err := loadConfig(be)
	if err != nil {
		return nil, err
	}
	s := &Store{backend: be, seen: map[string]bool{}}

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
		// data is not. Fail closed if we cannot tell whether it is established.
		if !create {
			return nil, ErrNotEncrypted
		}
		has, err := established(be)
		if err != nil {
			return nil, err
		}
		if has {
			return nil, ErrNotEncrypted
		}
		newCfg, enc, err := newEncryptedConfig(passphrase)
		if err != nil {
			return nil, err
		}
		if err := saveConfig(be, newCfg); err != nil {
			return nil, err
		}
		s.enc = enc
	}
	return s, nil
}

// established reports whether a vault already holds at least one snapshot, so a
// passphrase cannot be used to "encrypt" a vault that already has plaintext data
// in it. It returns an error rather than a guess when the store can't be listed,
// so the caller fails closed instead of clobbering existing data.
func established(be backend) (bool, error) {
	objs, err := be.list("snapshots/")
	if err != nil {
		return false, err
	}
	for _, o := range objs {
		if strings.HasSuffix(o.key, ".json") {
			return true, nil
		}
	}
	return false, nil
}

// chunkKey is the storage key for a chunk, sharding by the first two characters
// of its ID to avoid one enormous directory or prefix.
func chunkKey(id string) string {
	return "chunks/" + id[:2] + "/" + id
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

// PutChunk stores data under its content ID and returns that ID. The ID is taken
// over the uncompressed plaintext (see chunkID), so deduplication is unaffected
// by compression or encryption. wasNew is false when the chunk already existed —
// a deduplication hit — and stored is the number of bytes written (0 on a dedup
// hit).
//
// Each chunk is compressed and then, for an encrypted vault, encrypted before it
// is written. It is safe to call concurrently: a chunk is claimed under a lock
// before the (possibly remote) existence check and write, so exactly one caller
// writes any given chunk.
func (s *Store) PutChunk(data []byte) (id string, wasNew bool, stored int, err error) {
	id = s.chunkID(data)
	claim := id // id is a named return the error paths overwrite; capture the key to release.

	s.mu.Lock()
	if s.seen[id] {
		s.mu.Unlock()
		return id, false, 0, nil // another chunk with this content already handled it
	}
	s.seen[id] = true // claim it; we are the one writer
	s.mu.Unlock()

	// Release the claim on failure so a retry can store the chunk. The claim is
	// held across the existence check and write, which run outside the lock so
	// chunks store in parallel.
	defer func() {
		if err != nil {
			s.mu.Lock()
			delete(s.seen, claim)
			s.mu.Unlock()
		}
	}()

	key := chunkKey(id)
	present, err := s.backend.exists(key)
	if err != nil {
		return "", false, 0, err
	}
	if present {
		return id, false, 0, nil // already stored from a previous run — dedup
	}

	blob, err := encodeChunk(s.codec, data)
	if err != nil {
		return "", false, 0, err
	}
	if s.enc != nil {
		if blob, err = s.enc.seal(blob); err != nil {
			return "", false, 0, err
		}
	}
	if err = s.backend.put(key, blob); err != nil {
		return "", false, 0, err
	}
	return id, true, len(blob), nil
}

// GetChunk returns the original contents of the chunk with the given ID,
// decrypting it (for an encrypted vault) and decompressing it. It verifies that
// the recovered bytes reproduce the ID the chunk is stored under, so a corrupted,
// tampered, or undecodable chunk is reported as an integrity failure rather than
// restored.
func (s *Store) GetChunk(id string) ([]byte, error) {
	raw, err := s.backend.get(chunkKey(id))
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

// ListSnapshots returns the IDs of every snapshot in the vault, sorted oldest
// first (IDs lead with a timestamp, so a lexical sort is chronological).
func (s *Store) ListSnapshots() ([]string, error) {
	objs, err := s.backend.list("snapshots/")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, o := range objs {
		name := strings.TrimPrefix(o.key, "snapshots/")
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(ids)
	return ids, nil
}
