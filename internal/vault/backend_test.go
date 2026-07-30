package vault

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// memBackend is an in-memory backend used to prove the Store depends only on the
// backend interface, not on the filesystem.
type memBackend struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemBackend() *memBackend { return &memBackend{m: map[string][]byte{}} }

func (b *memBackend) open(bool) error { return nil }

func (b *memBackend) get(key string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.m[key]
	if !ok {
		return nil, fmt.Errorf("%s: %w", key, errNotFound)
	}
	return append([]byte(nil), data...), nil
}

func (b *memBackend) put(key string, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.m[key] = append([]byte(nil), data...)
	return nil
}

func (b *memBackend) exists(key string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.m[key]
	return ok, nil
}

func (b *memBackend) list(prefix string) ([]objectInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []objectInfo
	for k, v := range b.m {
		if strings.HasPrefix(k, prefix) {
			out = append(out, objectInfo{key: k, size: int64(len(v))})
		}
	}
	return out, nil
}

// testBackendContract exercises the behaviour every backend must provide.
func testBackendContract(t *testing.T, be backend) {
	t.Helper()
	if err := be.open(true); err != nil {
		t.Fatalf("open: %v", err)
	}

	if _, err := be.get("chunks/aa/x"); !errors.Is(err, errNotFound) {
		t.Fatalf("get missing: want errNotFound, got %v", err)
	}
	if ok, err := be.exists("chunks/aa/x"); err != nil || ok {
		t.Fatalf("exists missing: ok=%v err=%v", ok, err)
	}

	want := []byte("hello backend")
	if err := be.put("chunks/aa/x", want); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got, err := be.get("chunks/aa/x"); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("get after put: got %q err %v", got, err)
	}
	if ok, _ := be.exists("chunks/aa/x"); !ok {
		t.Fatal("exists should be true after put")
	}

	// put overwrites in place.
	if err := be.put("chunks/aa/x", []byte("replaced")); err != nil {
		t.Fatal(err)
	}
	if got, _ := be.get("chunks/aa/x"); string(got) != "replaced" {
		t.Fatalf("overwrite: got %q", got)
	}

	// list is prefix-scoped and reports sizes.
	_ = be.put("snapshots/1.json", []byte("s1"))
	_ = be.put("snapshots/2.json", []byte("s2"))
	_ = be.put("chunks/bb/y", []byte("yy"))
	if snaps, err := be.list("snapshots/"); err != nil || len(snaps) != 2 {
		t.Fatalf("list snapshots: got %d err %v", len(snaps), err)
	}
	chunks, err := be.list("chunks/")
	if err != nil || len(chunks) != 2 {
		t.Fatalf("list chunks: got %d err %v", len(chunks), err)
	}
	for _, o := range chunks {
		if o.size == 0 {
			t.Errorf("chunk %s reported size 0", o.key)
		}
	}
	if empty, err := be.list("nothing/"); err != nil || len(empty) != 0 {
		t.Fatalf("list of an absent prefix should be empty: got %d err %v", len(empty), err)
	}
}

func TestLocalBackendContract(t *testing.T) { testBackendContract(t, &localBackend{root: t.TempDir()}) }
func TestMemBackendContract(t *testing.T)   { testBackendContract(t, newMemBackend()) }

// TestStoreOverMemBackend confirms the Store round-trips a chunk over a backend
// that never touches the filesystem, proving the abstraction holds.
func TestStoreOverMemBackend(t *testing.T) {
	s := &Store{backend: newMemBackend(), codec: Zstd, seen: map[string]bool{}}

	data := bytes.Repeat([]byte("chunkvault "), 500)
	id, wasNew, stored, err := s.PutChunk(data)
	if err != nil || !wasNew || stored == 0 {
		t.Fatalf("PutChunk: id=%q new=%v stored=%d err=%v", id, wasNew, stored, err)
	}
	got, err := s.GetChunk(id)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("GetChunk round-trip: err=%v", err)
	}
	if _, wasNew, _, _ := s.PutChunk(data); wasNew {
		t.Fatal("second PutChunk of identical data should deduplicate")
	}
}
