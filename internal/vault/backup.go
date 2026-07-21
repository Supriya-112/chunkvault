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
	Reused      int // files reused unchanged from the parent snapshot (not re-read)
	Skipped     int // non-regular entries not backed up (symlinks, devices, etc.)
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

	// Incremental backup: files unchanged since the most recent snapshot of the
	// same source are reused without being re-read. A first backup has no parent.
	parent, err := parentIndex(store, sourceDir)
	if err != nil {
		return nil, err
	}

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

	// The producer walks the tree, records file and directory metadata into
	// snap in walk order, and feeds each file's chunks into the pool.
	prod := &producer{ctx: ctx, jobs: jobs, source: sourceDir, chunkSize: chunkSize, snap: snap, parent: parent}
	produceErr := make(chan error, 1)
	go func() {
		defer close(jobs)
		produceErr <- prod.walk()
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
	res.Reused = prod.reused
	res.Skipped = prod.skipped
	return res, nil
}

// parentIndex returns the files of the most recent snapshot of source, keyed by
// relative path, for incremental reuse. It returns nil (not an error) when no
// prior snapshot of that source exists.
func parentIndex(store *Store, source string) (map[string]FileEntry, error) {
	ids, err := store.ListSnapshots()
	if err != nil {
		return nil, err
	}
	// ListSnapshots is sorted oldest-first (IDs lead with a timestamp), so scan
	// from the newest for one that backed up this same source.
	for i := len(ids) - 1; i >= 0; i-- {
		snap, err := store.LoadSnapshot(ids[i])
		if err != nil {
			return nil, err
		}
		if snap.Source != source {
			continue
		}
		index := make(map[string]FileEntry, len(snap.Files))
		for _, fe := range snap.Files {
			index[fe.Path] = fe
		}
		return index, nil
	}
	return nil, nil
}

// producer walks the source tree, records directory and file metadata into
// snap in walk order, and feeds each file's chunks into the worker pool. Files
// unchanged since the parent snapshot are reused without being re-read.
type producer struct {
	ctx       context.Context
	jobs      chan<- chunkJob
	source    string
	chunkSize int
	snap      *Snapshot
	parent    map[string]FileEntry // parent snapshot's files by relative path; nil if none

	reused  int // files reused unchanged from the parent
	skipped int // non-regular entries not backed up
}

// walk runs the tree walk. It stops early and returns ctx.Err() if cancelled.
func (p *producer) walk() error {
	return filepath.WalkDir(p.source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(p.source, path)
		if err != nil {
			return err
		}

		if d.IsDir() {
			if rel != "." { // "." is the target dir itself, created on restore
				p.snap.Dirs = append(p.snap.Dirs, DirEntry{Path: rel, Mode: uint32(info.Mode())})
			}
			return nil
		}
		if !d.Type().IsRegular() {
			p.skipped++ // symlinks, devices, sockets, etc. are not backed up
			return nil
		}

		entry := FileEntry{Path: rel, Size: info.Size(), Mode: uint32(info.Mode()), ModTime: info.ModTime().UnixNano()}

		// Incremental: if size and mtime match the parent snapshot, the content
		// is unchanged, so reuse its chunk list instead of re-reading the file.
		if prev, ok := p.parent[rel]; ok && prev.Size == entry.Size && prev.ModTime == entry.ModTime {
			entry.Chunks = append([]string(nil), prev.Chunks...)
			p.snap.Files = append(p.snap.Files, entry)
			p.reused++
			return nil
		}

		fileIdx := len(p.snap.Files)
		p.snap.Files = append(p.snap.Files, entry)

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		index := 0
		return chunk.Split(f, p.chunkSize, func(data []byte) error {
			if err := p.ctx.Err(); err != nil {
				return err
			}
			// Split reuses its buffer once this returns, so copy before the
			// chunk travels to a worker.
			buf := make([]byte, len(data))
			copy(buf, data)
			select {
			case p.jobs <- chunkJob{file: fileIdx, index: index, data: buf}:
				index++
				return nil
			case <-p.ctx.Done():
				return p.ctx.Err()
			}
		})
	})
}
