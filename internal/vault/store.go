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
)

// Store is a content-addressable chunk store rooted at a directory.
type Store struct {
	root string
}

// Open opens (creating if needed) a vault at root.
func Open(root string) (*Store, error) {
	for _, sub := range []string{"chunks", "snapshots"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", sub, err)
		}
	}
	return &Store{root: root}, nil
}

// chunkPath returns the on-disk path for a chunk, sharding by the first two
// hex characters of its hash to avoid huge single directories.
func (s *Store) chunkPath(hash string) string {
	return filepath.Join(s.root, "chunks", hash[:2], hash)
}

// HasChunk reports whether a chunk with the given hash is already stored.
func (s *Store) HasChunk(hash string) bool {
	_, err := os.Stat(s.chunkPath(hash))
	return err == nil
}

// PutChunk stores data under the hash of its contents and returns that hash.
// wasNew is false when the chunk already existed — i.e. a deduplication hit.
// The write is atomic: content goes to a temp file that is renamed into place.
func (s *Store) PutChunk(data []byte) (hash string, wasNew bool, err error) {
	sum := sha256.Sum256(data)
	hash = hex.EncodeToString(sum[:])

	dst := s.chunkPath(hash)
	if _, statErr := os.Stat(dst); statErr == nil {
		return hash, false, nil // already have it — dedup
	}

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
