package vault

import (
	"sync"
	"sync/atomic"
)

// Progress accumulates live counters for a running backup or verify. It is safe
// for concurrent use, and every mutator is a no-op on a nil *Progress, so the
// engine updates it unconditionally while callers that don't want a live view
// simply leave it nil.
type Progress struct {
	done  atomic.Int64 // work completed, drives the bar: bytes read (backup) or chunks checked (verify)
	total atomic.Int64 // total work; 0 while unknown

	files       atomic.Int64
	chunks      atomic.Int64
	newChunks   atomic.Int64
	storedBytes atomic.Int64
	corrupt     atomic.Int64
	missing     atomic.Int64

	mu      sync.Mutex
	current string
}

// NewProgress returns a ready-to-use Progress.
func NewProgress() *Progress { return &Progress{} }

func (p *Progress) setTotal(n int64) {
	if p == nil {
		return
	}
	p.total.Store(n)
}

// readBytes advances the backup bar as source bytes are read.
func (p *Progress) readBytes(n int64) {
	if p == nil {
		return
	}
	p.done.Add(n)
}

// fileDone marks a freshly-read file complete.
func (p *Progress) fileDone(rel string) {
	if p == nil {
		return
	}
	p.files.Add(1)
	p.mu.Lock()
	p.current = rel
	p.mu.Unlock()
}

// reusedFile marks an incrementally-reused file complete; its bytes were not
// read, so advance the bar by its size directly.
func (p *Progress) reusedFile(rel string, size int64) {
	if p == nil {
		return
	}
	p.files.Add(1)
	p.done.Add(size)
	p.mu.Lock()
	p.current = rel
	p.mu.Unlock()
}

// chunkStored records one stored chunk (backup).
func (p *Progress) chunkStored(isNew bool, stored int) {
	if p == nil {
		return
	}
	p.chunks.Add(1)
	if isNew {
		p.newChunks.Add(1)
		p.storedBytes.Add(int64(stored))
	}
}

// chunkChecked records one verified chunk (verify), advancing the bar.
func (p *Progress) chunkChecked(ok bool) {
	if p == nil {
		return
	}
	p.done.Add(1)
	if !ok {
		p.corrupt.Add(1)
	}
}

// addMissing records chunks a snapshot referenced but that were absent (verify).
func (p *Progress) addMissing(n int) {
	if p == nil {
		return
	}
	p.missing.Add(int64(n))
}

// ProgressSnapshot is a consistent read of a Progress for rendering.
type ProgressSnapshot struct {
	Done, Total              int64
	Files, Chunks, NewChunks int64
	StoredBytes              int64
	Corrupt, Missing         int64
	Current                  string
}

// Snapshot returns the current counters.
func (p *Progress) Snapshot() ProgressSnapshot {
	p.mu.Lock()
	cur := p.current
	p.mu.Unlock()
	return ProgressSnapshot{
		Done:        p.done.Load(),
		Total:       p.total.Load(),
		Files:       p.files.Load(),
		Chunks:      p.chunks.Load(),
		NewChunks:   p.newChunks.Load(),
		StoredBytes: p.storedBytes.Load(),
		Corrupt:     p.corrupt.Load(),
		Missing:     p.missing.Load(),
		Current:     cur,
	}
}

// Fraction is the completed share of the work in [0,1], or 0 when the total is
// not yet known.
func (s ProgressSnapshot) Fraction() float64 {
	if s.Total <= 0 {
		return 0
	}
	if s.Done >= s.Total {
		return 1
	}
	return float64(s.Done) / float64(s.Total)
}
