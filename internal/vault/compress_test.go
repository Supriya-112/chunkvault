package vault

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseCodec(t *testing.T) {
	for in, want := range map[string]Codec{
		"none":  NoCompression,
		"store": NoCompression,
		"gzip":  Gzip,
		"zstd":  Zstd,
	} {
		got, err := ParseCodec(in)
		if err != nil {
			t.Errorf("ParseCodec(%q): unexpected error %v", in, err)
		}
		if got != want {
			t.Errorf("ParseCodec(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := ParseCodec("lz4"); err == nil {
		t.Error("ParseCodec should reject an unknown codec name")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	payloads := map[string][]byte{
		"empty":        {},
		"small":        []byte("hello"),
		"compressible": bytes.Repeat([]byte("chunkvault "), 2000),
		"binary":       pseudoRandom(9, 4096),
	}
	for _, codec := range []Codec{NoCompression, Gzip, Zstd} {
		for name, data := range payloads {
			encoded, err := encodeChunk(codec, data)
			if err != nil {
				t.Fatalf("encodeChunk(%v, %s): %v", codec, name, err)
			}
			got, err := decodeChunk(encoded)
			if err != nil {
				t.Fatalf("decodeChunk(%v, %s): %v", codec, name, err)
			}
			if !bytes.Equal(got, data) {
				t.Errorf("round-trip mismatch for codec %v, payload %s", codec, name)
			}
		}
	}
}

// TestCompressibleDataShrinks checks that compressible data is actually stored
// smaller, and that its recorded codec is the one that was requested.
func TestCompressibleDataShrinks(t *testing.T) {
	data := bytes.Repeat([]byte("the quick brown fox "), 1000)
	for _, codec := range []Codec{Gzip, Zstd} {
		encoded, err := encodeChunk(codec, data)
		if err != nil {
			t.Fatalf("encodeChunk(%v): %v", codec, err)
		}
		if len(encoded) >= len(data) {
			t.Errorf("codec %v did not shrink compressible data: %d -> %d", codec, len(data), len(encoded))
		}
		if Codec(encoded[0]) != codec {
			t.Errorf("codec %v: recorded tag = %d, want %d", codec, encoded[0], byte(codec))
		}
	}
}

// TestIncompressibleStoredRaw checks the fallback: data that does not compress
// is stored verbatim so a chunk never grows on disk.
func TestIncompressibleStoredRaw(t *testing.T) {
	data := pseudoRandom(11, 16384) // high entropy: won't compress
	for _, codec := range []Codec{Gzip, Zstd} {
		encoded, err := encodeChunk(codec, data)
		if err != nil {
			t.Fatalf("encodeChunk(%v): %v", codec, err)
		}
		if Codec(encoded[0]) != NoCompression {
			t.Errorf("codec %v: incompressible data should fall back to NoCompression, got tag %d", codec, encoded[0])
		}
		if len(encoded) != len(data)+1 {
			t.Errorf("codec %v: stored %d bytes for %d bytes of raw data (want +1 header)", codec, len(encoded), len(data))
		}
	}
}

func TestDecodeChunkRejectsGarbage(t *testing.T) {
	if _, err := decodeChunk(nil); err == nil {
		t.Error("decodeChunk should reject an empty chunk")
	}
	if _, err := decodeChunk([]byte{0x7f, 1, 2, 3}); err == nil {
		t.Error("decodeChunk should reject an unknown codec tag")
	}
}

// TestPutChunkCompresses checks that the store compresses compressible chunks
// on disk while returning the original bytes on read.
func TestPutChunkCompresses(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store.codec = Zstd

	data := bytes.Repeat([]byte("chunkvault compresses well "), 2000)
	hash, wasNew, stored, err := store.PutChunk(data)
	if err != nil {
		t.Fatalf("PutChunk: %v", err)
	}
	if !wasNew {
		t.Fatal("first PutChunk should be new")
	}
	if stored >= len(data) {
		t.Fatalf("expected compressed storage, stored %d bytes for %d bytes of data", stored, len(data))
	}

	got, err := store.GetChunk(hash)
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("GetChunk did not return the original bytes")
	}
}

// TestGetChunkDetectsCorruption confirms a chunk whose file was corrupted (so it
// no longer decodes to its hash) is reported as an integrity failure.
func TestGetChunkDetectsCorruption(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store.codec = Zstd

	hash, _, _, err := store.PutChunk(bytes.Repeat([]byte("data "), 400))
	if err != nil {
		t.Fatalf("PutChunk: %v", err)
	}
	if err := store.backend.put(chunkKey(hash), []byte("corrupted")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetChunk(hash); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("expected an integrity error, got %v", err)
	}
}
