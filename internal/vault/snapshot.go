package vault

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// newSnapshotID returns a sortable, collision-resistant snapshot ID: a UTC
// timestamp plus a short random suffix so two backups in the same second do
// not clobber each other.
func newSnapshotID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:])
}

// FileEntry records one backed-up file and the ordered hashes of its chunks.
type FileEntry struct {
	Path    string   `json:"path"`   // path relative to the backup source root
	Size    int64    `json:"size"`   // size in bytes
	Mode    uint32   `json:"mode"`   // unix file mode bits
	ModTime int64    `json:"mtime"`  // modification time, unix nanoseconds
	Chunks  []string `json:"chunks"` // ordered chunk hashes; concatenate to rebuild
}

// DirEntry records one backed-up directory so empty directories survive a
// round-trip and directory permissions are restored.
type DirEntry struct {
	Path string `json:"path"` // path relative to the backup source root
	Mode uint32 `json:"mode"` // unix file mode bits
}

// Snapshot is the manifest for one backup run. Dirs is ordered parents-first
// (WalkDir order), so restoring them in order recreates the tree.
type Snapshot struct {
	ID     string      `json:"id"`
	Source string      `json:"source"`
	Dirs   []DirEntry  `json:"dirs,omitempty"`
	Files  []FileEntry `json:"files"`
}

// SaveSnapshot writes a snapshot manifest into the vault as pretty JSON.
func (s *Store) SaveSnapshot(snap *Snapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.root, "snapshots", snap.ID+".json")
	return os.WriteFile(path, data, 0o644)
}

// LoadSnapshot reads a snapshot manifest by its ID.
func (s *Store) LoadSnapshot(id string) (*Snapshot, error) {
	path := filepath.Join(s.root, "snapshots", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("loading snapshot %q: %w", id, err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("parsing snapshot %q: %w", id, err)
	}
	return &snap, nil
}
