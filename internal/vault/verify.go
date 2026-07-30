package vault

import (
	"context"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// VerifyReport summarizes an integrity check.
type VerifyReport struct {
	Chunks    int      // chunks whose content was checked (deep) or existence confirmed (quick)
	Snapshots int      // snapshots whose chunk references were checked
	Corrupt   []string // chunk IDs whose stored bytes failed to decode back to their ID
	Missing   []string // chunk IDs referenced by a snapshot but absent from the store
	Broken    []string // snapshot IDs that reference a corrupt or missing chunk
}

// OK reports whether the vault passed: no corrupt and no missing chunks.
func (r *VerifyReport) OK() bool {
	return len(r.Corrupt) == 0 && len(r.Missing) == 0
}

// Verify checks the integrity of a vault. With an empty snapshotID it checks
// every stored chunk and every snapshot; otherwise it checks just the named
// snapshot's chunks. When quick is false (the default) each chunk's stored bytes
// are decrypted, decompressed, and re-hashed to detect bit-rot or tampering;
// when quick is true only the presence of each referenced chunk is checked.
// workers <= 0 uses one per CPU. An encrypted vault requires its passphrase.
// A non-nil progress receives live counters. Cancelling ctx aborts a deep check
// and returns ctx.Err().
func Verify(ctx context.Context, vaultDir, snapshotID string, passphrase []byte, workers int, quick bool, progress *Progress) (*VerifyReport, error) {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	store, err := openStore(vaultDir, passphrase, false)
	if err != nil {
		return nil, err
	}
	rep := &VerifyReport{}

	if snapshotID != "" {
		return rep, store.verifyOne(ctx, snapshotID, workers, quick, progress, rep)
	}
	return rep, store.verifyAll(ctx, workers, quick, progress, rep)
}

// verifyOne checks a single snapshot: which of its chunks are missing, and
// (unless quick) which present ones fail to decode.
func (s *Store) verifyOne(ctx context.Context, snapshotID string, workers int, quick bool, progress *Progress, rep *VerifyReport) error {
	snap, err := s.LoadSnapshot(snapshotID)
	if err != nil {
		return err
	}
	rep.Snapshots = 1

	missing, present, err := s.partitionByPresence(uniqueRefs(snap))
	if err != nil {
		return err
	}
	rep.Missing = missing
	rep.Chunks = len(present)
	progress.setTotal(int64(len(present)))

	if !quick {
		corrupt, err := s.verifyChunks(ctx, present, workers, progress)
		if err != nil {
			return err
		}
		rep.Corrupt = corrupt
	}
	progress.addMissing(len(missing))
	if len(rep.Missing) > 0 || len(rep.Corrupt) > 0 {
		rep.Broken = []string{snapshotID}
	}
	return nil
}

// verifyAll checks every stored chunk and every snapshot in the vault.
func (s *Store) verifyAll(ctx context.Context, workers int, quick bool, progress *Progress, rep *VerifyReport) error {
	ids, err := s.listChunkIDs()
	if err != nil {
		return err
	}
	rep.Chunks = len(ids)
	progress.setTotal(int64(len(ids)))

	present := make(map[string]bool, len(ids))
	for _, id := range ids {
		present[id] = true
	}
	corrupt := map[string]bool{}
	if !quick {
		bad, err := s.verifyChunks(ctx, ids, workers, progress)
		if err != nil {
			return err
		}
		rep.Corrupt = bad
		for _, id := range bad {
			corrupt[id] = true
		}
	}

	snapIDs, err := s.ListSnapshots()
	if err != nil {
		return err
	}
	rep.Snapshots = len(snapIDs)

	missing := map[string]bool{}
	for _, sid := range snapIDs {
		snap, err := s.LoadSnapshot(sid)
		if err != nil {
			return err
		}
		broken := false
		for _, id := range uniqueRefs(snap) {
			if !present[id] {
				missing[id] = true
				broken = true
			} else if corrupt[id] {
				broken = true
			}
		}
		if broken {
			rep.Broken = append(rep.Broken, sid)
		}
	}
	progress.addMissing(len(missing))
	rep.Missing = sortedKeys(missing)
	sort.Strings(rep.Broken)
	return nil
}

// verifyChunks decodes each chunk across a worker pool and returns the IDs that
// failed. A corrupt, truncated, or tampered chunk surfaces as a GetChunk error.
func (s *Store) verifyChunks(ctx context.Context, ids []string, workers int, progress *Progress) ([]string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan string)
	var mu sync.Mutex
	var corrupt []string

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				if ctx.Err() != nil {
					continue // draining after cancellation
				}
				_, err := s.GetChunk(id)
				progress.chunkChecked(err == nil)
				if err != nil {
					mu.Lock()
					corrupt = append(corrupt, id)
					mu.Unlock()
				}
			}
		}()
	}

	var feedErr error
	for _, id := range ids {
		select {
		case jobs <- id:
		case <-ctx.Done():
			feedErr = ctx.Err()
		}
		if feedErr != nil {
			break
		}
	}
	close(jobs)
	wg.Wait()
	if feedErr != nil {
		return nil, feedErr
	}
	sort.Strings(corrupt)
	return corrupt, nil
}

// listChunkIDs returns the ID of every chunk stored in the vault.
func (s *Store) listChunkIDs() ([]string, error) {
	objs, err := s.backend.list("chunks/")
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(objs))
	for _, o := range objs {
		ids = append(ids, o.key[strings.LastIndex(o.key, "/")+1:])
	}
	return ids, nil
}

// partitionByPresence splits chunk IDs into those absent from and those present
// in the store. A backend error (as opposed to a genuine absence) is returned,
// so a transient failure is not misreported as a missing chunk. The missing list
// is sorted for stable reporting.
func (s *Store) partitionByPresence(ids []string) (missing, present []string, err error) {
	for _, id := range ids {
		ok, err := s.backend.exists(chunkKey(id))
		if err != nil {
			return nil, nil, err
		}
		if ok {
			present = append(present, id)
		} else {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing, present, nil
}

// uniqueRefs returns the distinct chunk IDs a snapshot references, in first-seen
// order.
func uniqueRefs(snap *Snapshot) []string {
	seen := map[string]bool{}
	var out []string
	for _, fe := range snap.Files {
		for _, id := range fe.Chunks {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// sortedKeys returns the keys of set in sorted order.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
