package chunk

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

// pseudoRandom returns n deterministic, well-mixed bytes (splitmix64), so tests
// exercise varied data without depending on a random seed.
func pseudoRandom(n int) []byte {
	out := make([]byte, n)
	x := uint64(0x123456789)
	for i := range out {
		x += 0x9e3779b97f4a7c15
		z := x
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		out[i] = byte(z >> 24)
	}
	return out
}

func chunkHashes(t *testing.T, data []byte, avg int) [][32]byte {
	t.Helper()
	var hashes [][32]byte
	err := Split(bytes.NewReader(data), avg, func(c []byte) error {
		sum := sha256.Sum256(c)
		hashes = append(hashes, sum)
		return nil
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	return hashes
}

func TestSplitReassembles(t *testing.T) {
	data := pseudoRandom(1 << 18) // 256 KiB
	const avg = 1024
	min, max := avg/4, avg*4

	var got []byte
	var sizes []int
	err := Split(bytes.NewReader(data), avg, func(c []byte) error {
		got = append(got, c...)
		sizes = append(sizes, len(c))
		return nil
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Fatal("reassembled data does not match original")
	}
	if len(sizes) < 2 {
		t.Fatalf("expected the input to split into several chunks, got %d", len(sizes))
	}
	for i, s := range sizes {
		if s > max {
			t.Errorf("chunk %d size %d exceeds max %d", i, s, max)
		}
		// Only the final chunk may be shorter than the minimum.
		if s < min && i != len(sizes)-1 {
			t.Errorf("chunk %d size %d below min %d", i, s, min)
		}
	}
}

func TestSplitIsDeterministic(t *testing.T) {
	data := pseudoRandom(1 << 16)
	a := chunkHashes(t, data, 1024)
	b := chunkHashes(t, data, 1024)
	if len(a) != len(b) {
		t.Fatalf("chunk count differs between runs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("chunk %d differs between runs on identical input", i)
		}
	}
}

// TestSplitBoundariesAreStableUnderInsertion is the reason for content-defined
// chunking: inserting bytes in the middle of a file should leave the chunks on
// either side of the edit intact. Fixed-size chunking would instead shift every
// chunk after the insertion point.
func TestSplitBoundariesAreStableUnderInsertion(t *testing.T) {
	data := pseudoRandom(1 << 18)
	const avg = 1024

	before := chunkHashes(t, data, avg)

	mid := len(data) / 2
	edited := make([]byte, 0, len(data)+64)
	edited = append(edited, data[:mid]...)
	edited = append(edited, pseudoRandom(64)...)
	edited = append(edited, data[mid:]...)
	after := chunkHashes(t, edited, avg)

	have := map[[32]byte]int{}
	for _, h := range after {
		have[h]++
	}
	shared := 0
	for _, h := range before {
		if have[h] > 0 {
			have[h]--
			shared++
		}
	}

	if shared < len(before)*3/4 {
		t.Fatalf("only %d of %d chunks survived a mid-file insertion; boundaries are not content-defined", shared, len(before))
	}
}

// TestSplitProgressesOnTinyAvgSize guards against a regression where an avgSize
// small enough to make the minimum chunk size 0 produced zero-length cuts and
// stalled Split. Every chunk must be non-empty and the input must reassemble.
func TestSplitProgressesOnTinyAvgSize(t *testing.T) {
	data := bytes.Repeat([]byte("abcdefgh"), 4096) // 32 KiB
	for _, avg := range []int{1, 2, 3, 4} {
		var got []byte
		chunks := 0
		err := Split(bytes.NewReader(data), avg, func(c []byte) error {
			if len(c) == 0 {
				t.Fatalf("avg=%d: zero-length chunk (no forward progress)", avg)
			}
			chunks++
			if chunks > len(data) {
				t.Fatalf("avg=%d: more chunks than bytes; Split is not advancing", avg)
			}
			got = append(got, c...)
			return nil
		})
		if err != nil {
			t.Fatalf("avg=%d: Split: %v", avg, err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("avg=%d: reassembled data does not match original", avg)
		}
	}
}

func TestSplitEmptyInput(t *testing.T) {
	var chunks int
	err := Split(bytes.NewReader(nil), 1024, func(c []byte) error {
		chunks++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunks != 0 {
		t.Fatalf("expected 0 chunks for empty input, got %d", chunks)
	}
}
