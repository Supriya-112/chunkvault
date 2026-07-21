package vault

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"github.com/Supriya-112/chunkvault/internal/chunk"
)

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

// chunkJob is one chunk waiting to be hashed and stored, tagged with its file
// and its position within that file so the manifest can be reassembled in order.
type chunkJob struct {
	file  int
	index int
	data  []byte
}

// chunkResult is the outcome of storing one chunk.
type chunkResult struct {
	file   int
	index  int
	hash   string
	wasNew bool
	size   int
	err    error
}

// Backup walks sourceDir, chunks every regular file, and stores unique chunks
// in the vault at vaultDir, then writes a snapshot manifest. Chunk hashing and
// writing run across a pool of workers; workers defaults to runtime.NumCPU()
// when <= 0. A chunkSize <= 0 uses the chunk package default. Cancelling ctx
// aborts the run and returns ctx.Err().
func Backup(ctx context.Context, sourceDir, vaultDir string, chunkSize, workers int) (*Result, error) {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	store, err := Open(vaultDir)
	if err != nil {
		return nil, err
	}

	// A local cancellable context so a store error in one worker stops the rest.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	snap := &Snapshot{ID: newSnapshotID(), Source: sourceDir}
	res := &Result{}

	jobs := make(chan chunkJob)
	results := make(chan chunkResult)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					continue // draining after cancellation; do no work
				}
				hash, wasNew, err := store.PutChunk(j.data)
				select {
				case results <- chunkResult{file: j.file, index: j.index, hash: hash, wasNew: wasNew, size: len(j.data), err: err}:
				case <-ctx.Done():
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	// The producer walks the tree, records file metadata into snap in walk
	// order, and feeds each file's chunks into the pool.
	produceErr := make(chan error, 1)
	go func() {
		defer close(jobs)
		produceErr <- splitTree(ctx, jobs, sourceDir, chunkSize, snap)
	}()

	// Collect results (this goroutine is the only writer of res, so no locking
	// is needed for the stats). Hashes are grouped per file and ordered later.
	type placed struct {
		index int
		hash  string
	}
	byFile := map[int][]placed{}
	var firstErr error
	for r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
				cancel()
			}
			continue
		}
		if firstErr != nil {
			continue
		}
		byFile[r.file] = append(byFile[r.file], placed{r.index, r.hash})
		res.TotalChunks++
		res.TotalBytes += int64(r.size)
		if r.wasNew {
			res.NewChunks++
			res.StoredBytes += int64(r.size)
		}
	}
	if perr := <-produceErr; perr != nil && firstErr == nil {
		firstErr = perr
	}
	if firstErr != nil {
		return nil, firstErr
	}

	for fi := range snap.Files {
		hs := byFile[fi]
		if len(hs) == 0 {
			continue
		}
		sort.Slice(hs, func(a, b int) bool { return hs[a].index < hs[b].index })
		chunks := make([]string, len(hs))
		for k, h := range hs {
			chunks[k] = h.hash
		}
		snap.Files[fi].Chunks = chunks
	}

	if err := store.SaveSnapshot(snap); err != nil {
		return nil, err
	}
	res.SnapshotID = snap.ID
	res.Files = len(snap.Files)
	return res, nil
}

// splitTree walks sourceDir, appends a manifest entry for each regular file, and
// sends that file's chunks to jobs. It stops early and returns ctx.Err() if ctx
// is cancelled.
func splitTree(ctx context.Context, jobs chan<- chunkJob, sourceDir string, chunkSize int, snap *Snapshot) error {
	return filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil // skip directories, symlinks, devices, etc.
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		fileIdx := len(snap.Files)
		snap.Files = append(snap.Files, FileEntry{Path: rel, Size: info.Size(), Mode: uint32(info.Mode())})

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		index := 0
		return chunk.Split(f, chunkSize, func(data []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			// Split reuses its buffer once this returns, so copy before the
			// chunk travels to a worker.
			buf := make([]byte, len(data))
			copy(buf, data)
			select {
			case jobs <- chunkJob{file: fileIdx, index: index, data: buf}:
				index++
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	})
}
