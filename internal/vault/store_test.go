package vault

import (
	"bytes"
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
	if !store.HasChunk(hash) {
		t.Fatal("HasChunk returned false for a chunk we just stored")
	}

	got, err := store.GetChunk(hash)
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, data)
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
