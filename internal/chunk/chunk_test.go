package chunk

import (
	"bytes"
	"testing"
)

func TestSplitReassembles(t *testing.T) {
	data := bytes.Repeat([]byte("chunkvault"), 1000) // 10,000 bytes
	const size = 512

	var got []byte
	var chunks int
	err := Split(bytes.NewReader(data), size, func(c []byte) error {
		if len(c) > size {
			t.Fatalf("chunk larger than size: %d > %d", len(c), size)
		}
		got = append(got, c...)
		chunks++
		return nil
	})
	if err != nil {
		t.Fatalf("Split returned error: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("reassembled data does not match original")
	}
	wantChunks := (len(data) + size - 1) / size // ceil division
	if chunks != wantChunks {
		t.Fatalf("got %d chunks, want %d", chunks, wantChunks)
	}
}

func TestSplitEmptyInput(t *testing.T) {
	var chunks int
	err := Split(bytes.NewReader(nil), 512, func(c []byte) error {
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
