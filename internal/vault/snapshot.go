package vault

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// SaveSnapshot writes a snapshot manifest into the vault. For an encrypted
// vault the manifest is encrypted too, so file paths, sizes, and the directory
// structure are not exposed on disk.
func (s *Store) SaveSnapshot(snap *Snapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if s.enc != nil {
		if data, err = s.enc.seal(data); err != nil {
			return err
		}
	}
	return s.backend.put("snapshots/"+snap.ID+".json", data)
}

// LoadSnapshot reads a snapshot manifest by its ID, decrypting it first when
// the vault is encrypted.
func (s *Store) LoadSnapshot(id string) (*Snapshot, error) {
	data, err := s.backend.get("snapshots/" + id + ".json")
	if err != nil {
		return nil, fmt.Errorf("loading snapshot %q: %w", id, err)
	}
	if s.enc != nil {
		if data, err = s.enc.open(data); err != nil {
			return nil, fmt.Errorf("decrypting snapshot %q: %w", id, err)
		}
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("parsing snapshot %q: %w", id, err)
	}
	// Bind the manifest to the ID it was requested under, so a snapshot object
	// that was swapped or rolled back under a different name is detected rather
	// than silently restored (chunks get the same protection via their ID).
	if snap.ID != id {
		return nil, fmt.Errorf("snapshot %q has mismatched id %q", id, snap.ID)
	}
	return &snap, nil
}
