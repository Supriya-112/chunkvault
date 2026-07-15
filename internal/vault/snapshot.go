package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileEntry records one backed-up file and the ordered hashes of its chunks.
type FileEntry struct {
	Path   string   `json:"path"`   // path relative to the backup source root
	Size   int64    `json:"size"`   // size in bytes
	Mode   uint32   `json:"mode"`   // unix file mode bits
	Chunks []string `json:"chunks"` // ordered chunk hashes; concatenate to rebuild
}

// Snapshot is the manifest for one backup run.
type Snapshot struct {
	ID     string      `json:"id"`
	Source string      `json:"source"`
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
