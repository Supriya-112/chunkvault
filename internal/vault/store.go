// Package vault implements the on-disk chunk store, snapshot format, and the
// backup operation that ties them together.
//
// Layout on disk:
//
//	<vault>/
//	  chunks/<aa>/<full-sha256>   one file per unique chunk, named by its hash
//	  snapshots/<id>.json         a manifest of files and their chunk hashes
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
	root string

	mu   sync.Mutex
	seen map[string]bool // hashes known to be stored, so concurrent PutChunk calls write each chunk once
}

// Open opens (creating if needed) a vault at root.
func Open(root string) (*Store, error) {
	for _, sub := range []string{"chunks", "snapshots"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", sub, err)
		}
	}
	return &Store{root: root, seen: map[string]bool{}}, nil
}

// openExisting opens a vault that must already exist, so a mistyped path is
// reported as an error instead of silently creating an empty vault.
func openExisting(root string) (*Store, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("vault %q: %w", root, err)
	}
	return Open(root)
}

// chunkPath returns the on-disk path for a chunk, sharding by the first two
// hex characters of its hash to avoid huge single directories.
func (s *Store) chunkPath(hash string) string {
	return filepath.Join(s.root, "chunks", hash[:2], hash)
}

// PutChunk stores data under the hash of its contents and returns that hash.
// wasNew is false when the chunk already existed — i.e. a deduplication hit.
// The write is atomic: content goes to a temp file that is renamed into place.
//
// It is safe to call concurrently: a chunk is claimed under a lock before the
// write, so exactly one caller writes any given chunk and reports wasNew.
func (s *Store) PutChunk(data []byte) (hash string, wasNew bool, err error) {
	sum := sha256.Sum256(data)
	hash = hex.EncodeToString(sum[:])
	dst := s.chunkPath(hash)

	s.mu.Lock()
	if s.seen[hash] {
		s.mu.Unlock()
		return hash, false, nil // another chunk with this content already handled it
	}
	if _, statErr := os.Stat(dst); statErr == nil {
		s.seen[hash] = true
		s.mu.Unlock()
		return hash, false, nil // already on disk from a previous run — dedup
	}
	s.seen[hash] = true // claim it; we are the one writer
	s.mu.Unlock()

	// The write happens outside the lock so chunks store in parallel. On
	// failure the claim is released so a retry can store the chunk.
	defer func() {
		if err != nil {
			s.mu.Lock()
			delete(s.seen, hash)
			s.mu.Unlock()
		}
	}()

	if err = os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", false, err
	}
	tmp := dst + ".tmp"
	if err = os.WriteFile(tmp, data, 0o644); err != nil {
		return "", false, err
	}
	if err = os.Rename(tmp, dst); err != nil {
		return "", false, err
	}
	return hash, true, nil
}

// GetChunk returns the contents of the chunk with the given hash.
func (s *Store) GetChunk(hash string) ([]byte, error) {
	return os.ReadFile(s.chunkPath(hash))
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
