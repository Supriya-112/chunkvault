package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RestoreResult summarizes a completed restore run.
type RestoreResult struct {
	Files int
	Bytes int64
}

// Restore rebuilds the files recorded in snapshotID into targetDir, pulling
// each chunk from the vault, verifying its integrity, and restoring file
// permissions. Chunk contents are checked against their expected hash, so a
// corrupted vault is detected rather than silently restored.
func Restore(vaultDir, snapshotID, targetDir string) (*RestoreResult, error) {
	store, err := Open(vaultDir)
	if err != nil {
		return nil, err
	}
	snap, err := store.LoadSnapshot(snapshotID)
	if err != nil {
		return nil, err
	}

	res := &RestoreResult{}
	for _, fe := range snap.Files {
		dst, err := safeJoin(targetDir, fe.Path)
		if err != nil {
			return nil, err
		}
		if err := restoreFile(store, dst, fe, res); err != nil {
			return nil, fmt.Errorf("restoring %s: %w", fe.Path, err)
		}
		res.Files++
	}
	return res, nil
}

// restoreFile writes a single file entry from its chunks.
func restoreFile(store *Store, dst string, fe FileEntry, res *RestoreResult) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, want := range fe.Chunks {
		data, err := store.GetChunk(want)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			return fmt.Errorf("chunk integrity check failed: want %s got %s", want, got)
		}
		if _, err := f.Write(data); err != nil {
			return err
		}
		res.Bytes += int64(len(data))
	}

	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, os.FileMode(fe.Mode))
}

// safeJoin joins targetDir and a relative path, rejecting any path that would
// escape targetDir (e.g. via "../"). This guards against a hostile snapshot.
func safeJoin(targetDir, rel string) (string, error) {
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("unsafe path in snapshot: %q", rel)
	}
	return filepath.Join(targetDir, clean), nil
}
