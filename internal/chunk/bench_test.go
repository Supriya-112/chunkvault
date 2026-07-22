package chunk

import (
	"bytes"
	"testing"
)

// BenchmarkSplit measures content-defined chunking throughput (the rolling hash
// and boundary search) over a fixed buffer. b.SetBytes makes the result read as
// MB/s.
func BenchmarkSplit(b *testing.B) {
	data := pseudoRandom(32 << 20) // 32 MiB
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Split(bytes.NewReader(data), DefaultSize, func([]byte) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}
