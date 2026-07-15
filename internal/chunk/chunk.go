// Package chunk splits a byte stream into chunks.
//
// M1 uses fixed-size chunking. In a later milestone this is replaced by
// content-defined chunking (a rolling hash) so that inserting bytes near the
// start of a file does not shift every subsequent chunk boundary.
package chunk

import "io"

// DefaultSize is the default fixed chunk size (1 MiB).
const DefaultSize = 1 << 20

// Split reads r and invokes fn once per chunk of up to size bytes, in order.
// The slice passed to fn is only valid until fn returns; copy it if you need
// to retain it. If size <= 0, DefaultSize is used.
func Split(r io.Reader, size int, fn func(data []byte) error) error {
	if size <= 0 {
		size = DefaultSize
	}
	buf := make([]byte, size)
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			if ferr := fn(buf[:n]); ferr != nil {
				return ferr
			}
		}
		switch err {
		case nil:
			// full chunk read; continue
		case io.EOF, io.ErrUnexpectedEOF:
			// io.EOF: nothing left. io.ErrUnexpectedEOF: last (short) chunk
			// was already handled above.
			return nil
		default:
			return err
		}
	}
}
