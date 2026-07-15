package vault

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Supriya-112/chunkvault/internal/chunk"
)

// newSnapshotID returns a sortable, collision-resistant snapshot ID: a UTC
// timestamp plus a short random suffix so two backups in the same second do
// not clobber each other.
func newSnapshotID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:])
}

// Result summarizes a completed backup run.
type Result struct {
	SnapshotID  string
	Files       int
	TotalChunks int
	NewChunks   int   // chunks actually written (not deduplicated)
	TotalBytes  int64 // logical bytes read from the source
	StoredBytes int64 // bytes newly written to the store
}

// DedupRatio returns the fraction of scanned bytes that were deduplicated
// (0.0 to 1.0). Returns 0 when nothing was read.
func (r Result) DedupRatio() float64 {
	if r.TotalBytes == 0 {
		return 0
	}
	return float64(r.TotalBytes-r.StoredBytes) / float64(r.TotalBytes)
}

// Backup walks sourceDir, chunks every regular file, stores unique chunks in
// the vault at vaultDir, and writes a snapshot manifest. A chunkSize <= 0 uses
// the chunk package default.
func Backup(sourceDir, vaultDir string, chunkSize int) (*Result, error) {
	store, err := Open(vaultDir)
	if err != nil {
		return nil, err
	}

	snap := &Snapshot{
		ID:     newSnapshotID(),
		Source: sourceDir,
	}
	res := &Result{}

	walkErr := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil // skip directories, symlinks, devices, etc.
		}
		entry, err := backupFile(store, sourceDir, path, d, chunkSize, res)
		if err != nil {
			return fmt.Errorf("backing up %s: %w", path, err)
		}
		snap.Files = append(snap.Files, entry)
		res.Files++
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	if err := store.SaveSnapshot(snap); err != nil {
		return nil, err
	}
	res.SnapshotID = snap.ID
	return res, nil
}

// backupFile chunks a single file into the store and returns its manifest entry.
func backupFile(store *Store, sourceDir, path string, d fs.DirEntry, chunkSize int, res *Result) (FileEntry, error) {
	info, err := d.Info()
	if err != nil {
		return FileEntry{}, err
	}
	rel, err := filepath.Rel(sourceDir, path)
	if err != nil {
		return FileEntry{}, err
	}

	f, err := os.Open(path)
	if err != nil {
		return FileEntry{}, err
	}
	defer f.Close()

	entry := FileEntry{Path: rel, Size: info.Size(), Mode: uint32(info.Mode())}
	err = chunk.Split(f, chunkSize, func(data []byte) error {
		hash, wasNew, err := store.PutChunk(data)
		if err != nil {
			return err
		}
		entry.Chunks = append(entry.Chunks, hash)
		res.TotalChunks++
		res.TotalBytes += int64(len(data))
		if wasNew {
			res.NewChunks++
			res.StoredBytes += int64(len(data))
		}
		return nil
	})
	return entry, err
}
