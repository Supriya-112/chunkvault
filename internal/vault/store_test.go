package vault

import (
	"bytes"
	"sync"
	"testing"
)

func TestPutChunkDeduplicates(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	data := []byte("hello chunkvault")

	h1, new1, err := store.PutChunk(data)
	if err != nil {
		t.Fatalf("first PutChunk: %v", err)
	}
	if !new1 {
		t.Fatal("first PutChunk should report the chunk as new")
	}

	h2, new2, err := store.PutChunk(data)
	if err != nil {
		t.Fatalf("second PutChunk: %v", err)
	}
	if new2 {
		t.Fatal("second PutChunk of identical data should be a dedup hit (wasNew=false)")
	}
	if h1 != h2 {
		t.Fatalf("identical data produced different hashes: %s vs %s", h1, h2)
	}
}

func TestPutGetRoundTrip(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	data := []byte("some bytes to store and read back")
	hash, _, err := store.PutChunk(data)
	if err != nil {
		t.Fatalf("PutChunk: %v", err)
	}

	got, err := store.GetChunk(hash)
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, data)
	}
}

// TestPutChunkConcurrentIdentical drives many goroutines storing identical
// content at once and asserts exactly one reports wasNew — the store must write
// each chunk once even under contention.
func TestPutChunkConcurrentIdentical(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data := bytes.Repeat([]byte("concurrent chunk"), 512)

	const goroutines = 64
	news := make([]bool, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, wasNew, err := store.PutChunk(data)
			news[i], errs[i] = wasNew, err // distinct indices: no shared writes
		}(i)
	}
	wg.Wait()

	count := 0
	for i := 0; i < goroutines; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: PutChunk: %v", i, errs[i])
		}
		if news[i] {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one writer to report wasNew, got %d", count)
	}
}

func TestDifferentDataDifferentHash(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	h1, _, _ := store.PutChunk([]byte("aaaa"))
	h2, _, _ := store.PutChunk([]byte("bbbb"))
	if h1 == h2 {
		t.Fatal("different data must not share a hash")
	}
}
