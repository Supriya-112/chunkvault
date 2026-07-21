package vault

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// Stats summarizes a vault: how much data has been backed up across all
// snapshots versus how much is actually stored after deduplication.
type Stats struct {
	Snapshots    int
	UniqueChunks int   // distinct chunks held in the store
	StoredBytes  int64 // bytes physically stored in the chunk store
	LogicalBytes int64 // total size of every file across every snapshot
	ChunkRefs    int   // chunk references across every snapshot, counting repeats
}

// SavedBytes is the number of bytes deduplication avoided storing.
func (s Stats) SavedBytes() int64 {
	return s.LogicalBytes - s.StoredBytes
}

// DedupRatio is the fraction of logical bytes that did not need to be stored
// (0.0 to 1.0), or 0 when nothing has been backed up.
func (s Stats) DedupRatio() float64 {
	if s.LogicalBytes == 0 {
		return 0
	}
	return float64(s.LogicalBytes-s.StoredBytes) / float64(s.LogicalBytes)
}

// ComputeStats scans a vault and reports its deduplication statistics.
func ComputeStats(vaultDir string) (*Stats, error) {
	store, err := openExisting(vaultDir)
	if err != nil {
		return nil, err
	}

	var st Stats
	chunksRoot := filepath.Join(store.root, "chunks")
	err = filepath.WalkDir(chunksRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasSuffix(path, ".tmp") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		st.UniqueChunks++
		st.StoredBytes += info.Size()
		return nil
	})
	if err != nil {
		return nil, err
	}

	ids, err := store.ListSnapshots()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		snap, err := store.LoadSnapshot(id)
		if err != nil {
			return nil, err
		}
		st.Snapshots++
		for _, fe := range snap.Files {
			st.LogicalBytes += fe.Size
			st.ChunkRefs += len(fe.Chunks)
		}
	}
	return &st, nil
}
