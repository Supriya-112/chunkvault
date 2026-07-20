// Package chunk splits a byte stream into content-defined chunks using a
// FastCDC-style gear rolling hash. Chunk boundaries are chosen from the data
// itself, so inserting or removing bytes shifts only the chunks around the edit
// instead of every chunk after it.
package chunk

import (
	"io"
	"math/bits"
)

// DefaultSize is the target average chunk size (1 MiB). Actual chunks vary with
// the data, bounded to [DefaultSize/4, DefaultSize*4].
const DefaultSize = 1 << 20

// gear maps each byte value to a 64-bit number that the rolling hash mixes in
// as it slides over the data. It is generated deterministically: chunk
// boundaries are part of the on-disk format, so the same input must chunk
// identically across runs and machines for deduplication to work.
var gear = buildGearTable()

func buildGearTable() [256]uint64 {
	var t [256]uint64
	// splitmix64 with a fixed seed — reproducible and well-distributed, with no
	// dependence on math/rand's internals.
	x := uint64(0x9e3779b97f4a7c15)
	for i := range t {
		x += 0x9e3779b97f4a7c15
		z := x
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		t[i] = z ^ (z >> 31)
	}
	return t
}

// Split reads r and invokes fn once per content-defined chunk, in order.
// avgSize sets the target average chunk size; if avgSize <= 0, DefaultSize is
// used. The slice passed to fn is only valid until fn returns; copy it if you
// need to retain it.
func Split(r io.Reader, avgSize int, fn func(data []byte) error) error {
	c := newChunker(avgSize)
	buf := make([]byte, 0, c.max)
	eof := false
	for {
		// Keep up to max bytes buffered so boundary() can look ahead a full
		// maximum-sized chunk.
		for !eof && len(buf) < c.max {
			start := len(buf)
			buf = buf[:c.max]
			n, err := r.Read(buf[start:])
			buf = buf[:start+n]
			switch err {
			case nil:
			case io.EOF:
				eof = true
			default:
				return err
			}
		}
		if len(buf) == 0 {
			return nil
		}
		cut := c.boundary(buf)
		if err := fn(buf[:cut]); err != nil {
			return err
		}
		buf = append(buf[:0], buf[cut:]...) // slide the remainder to the front
	}
}

type chunker struct {
	min    int
	normal int
	max    int
	maskS  uint64 // stricter mask used before the chunk reaches normal size
	maskL  uint64 // looser mask used after it
}

func newChunker(avgSize int) chunker {
	if avgSize <= 0 {
		avgSize = DefaultSize
	}
	// Normalized chunking: while a chunk is still below the average size, a
	// mask with more 1 bits makes a cut point rarer; past the average, a mask
	// with fewer 1 bits makes one likelier. This concentrates chunk sizes
	// around the average.
	b := bits.Len(uint(avgSize)) - 1 // floor(log2(avgSize))
	if b < 2 {
		b = 2
	}
	return chunker{
		min:    avgSize / 4,
		normal: avgSize,
		max:    avgSize * 4,
		maskS:  1<<uint(b+1) - 1,
		maskL:  1<<uint(b-1) - 1,
	}
}

// boundary returns the length of the next chunk at the front of data: the first
// content-defined cut point, or a size bound (min or max) if none is found. The
// hash starts after the first min bytes, so no cut lands before the minimum.
func (c chunker) boundary(data []byte) int {
	n := len(data)
	if n <= c.min {
		return n
	}
	if n > c.max {
		n = c.max
	}
	normal := c.normal
	if normal > n {
		normal = n
	}

	var fp uint64
	i := c.min
	for ; i < normal; i++ {
		fp = fp<<1 + gear[data[i]]
		if fp&c.maskS == 0 {
			return i
		}
	}
	for ; i < n; i++ {
		fp = fp<<1 + gear[data[i]]
		if fp&c.maskL == 0 {
			return i
		}
	}
	return n
}
