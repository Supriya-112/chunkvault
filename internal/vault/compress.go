package vault

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

// Codec identifies how a chunk's bytes are compressed on disk. Each stored
// chunk begins with a single byte holding its codec, so a vault can mix codecs
// freely and a restore knows how to decode every chunk regardless of the codec
// a later backup happened to use.
type Codec byte

const (
	// NoCompression stores chunk bytes verbatim. It is also the codec recorded
	// for a chunk whose contents did not compress smaller than the original.
	NoCompression Codec = iota
	// Gzip compresses chunks with compress/gzip (DEFLATE).
	Gzip
	// Zstd compresses chunks with Zstandard.
	Zstd
)

// String returns the flag-facing name of the codec.
func (c Codec) String() string {
	switch c {
	case NoCompression:
		return "none"
	case Gzip:
		return "gzip"
	case Zstd:
		return "zstd"
	default:
		return fmt.Sprintf("codec(%d)", byte(c))
	}
}

// ParseCodec maps a --compress flag value to a Codec. "none" (and its alias
// "store") disables compression.
func ParseCodec(s string) (Codec, error) {
	switch s {
	case "none", "store":
		return NoCompression, nil
	case "gzip":
		return Gzip, nil
	case "zstd":
		return Zstd, nil
	default:
		return 0, fmt.Errorf("unknown compression %q (want none, gzip, or zstd)", s)
	}
}

// maxDecompressedChunk bounds how large a single chunk may inflate to on read.
// A real chunk is at most a few MiB (the chunker caps chunks at avgSize*4); this
// far-larger ceiling exists only so a hostile or corrupted vault cannot make a
// tiny chunk decompress into gigabytes and exhaust memory before the integrity
// check runs. It is a var so tests can lower it. (Both codecs are bounded: gzip
// via an io.LimitReader below, zstd via the decoder's max-memory option.)
var maxDecompressedChunk int64 = 512 << 20

// A single shared encoder and decoder is the intended way to use this zstd
// package: both are safe for concurrent use, so the backup worker pool can hash
// and store chunks in parallel without one per goroutine.
var (
	zstdEncoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	zstdDecoder, _ = zstd.NewReader(nil, zstd.WithDecoderMaxMemory(uint64(maxDecompressedChunk)))
)

// encodeChunk returns the on-disk representation of data under codec c: a
// one-byte codec tag followed by the payload. When compression does not shrink
// the chunk (already-compressed data such as media or archives), it is stored
// verbatim so a chunk never grows on disk.
func encodeChunk(c Codec, data []byte) ([]byte, error) {
	if c == NoCompression {
		return append([]byte{byte(NoCompression)}, data...), nil
	}
	payload, err := compress(c, data)
	if err != nil {
		return nil, err
	}
	if len(payload) >= len(data) {
		return append([]byte{byte(NoCompression)}, data...), nil
	}
	return append([]byte{byte(c)}, payload...), nil
}

// decodeChunk reverses encodeChunk, returning a chunk's original bytes. A tag
// it does not recognise, or a payload it cannot decompress, is reported as an
// error so a corrupted chunk is caught rather than silently mis-restored.
func decodeChunk(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty chunk file")
	}
	c, payload := Codec(raw[0]), raw[1:]
	switch c {
	case NoCompression:
		return payload, nil
	case Gzip, Zstd:
		return decompress(c, payload)
	default:
		return nil, fmt.Errorf("unknown chunk codec %d", byte(c))
	}
}

// compress returns the compressed payload (no codec tag) for a compressible
// codec.
func compress(c Codec, data []byte) ([]byte, error) {
	switch c {
	case Gzip:
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case Zstd:
		return zstdEncoder.EncodeAll(data, nil), nil
	default:
		return nil, fmt.Errorf("codec %s is not a compressing codec", c)
	}
}

// decompress inflates a payload produced by compress.
func decompress(c Codec, payload []byte) ([]byte, error) {
	switch c {
	case Gzip:
		r, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		// Read one byte past the ceiling so an over-large payload is detectable
		// rather than silently truncated.
		out, err := io.ReadAll(io.LimitReader(r, maxDecompressedChunk+1))
		if err != nil {
			return nil, err
		}
		if int64(len(out)) > maxDecompressedChunk {
			return nil, fmt.Errorf("gzip chunk exceeds the %d-byte decompression limit", maxDecompressedChunk)
		}
		if err := r.Close(); err != nil {
			return nil, err
		}
		return out, nil
	case Zstd:
		return zstdDecoder.DecodeAll(payload, nil)
	default:
		return nil, fmt.Errorf("codec %s is not a compressing codec", c)
	}
}
