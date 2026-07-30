package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// errNotFound is returned by a backend's get when an object is absent, so
// callers can distinguish "missing" from other I/O errors.
var errNotFound = errors.New("object not found")

// objectInfo identifies a stored object and its size in bytes.
type objectInfo struct {
	key  string
	size int64
}

// backend is the storage abstraction beneath a vault: a flat namespace of
// forward-slash keys such as "config.json", "chunks/<aa>/<id>", and
// "snapshots/<id>.json". It is implemented by the local filesystem and by S3.
// Implementations must be safe for concurrent use.
type backend interface {
	// open prepares the vault: with create it is made ready for writing (the
	// local directory is created; an existing S3 bucket is required); otherwise
	// its existence is verified so a mistyped location is reported.
	open(create bool) error
	// get returns the object at key, or a wrapped errNotFound if it is absent.
	get(key string) ([]byte, error)
	// put atomically writes data at key, replacing any existing object.
	put(key string, data []byte) error
	// exists reports whether an object is present at key.
	exists(key string) (bool, error)
	// list returns every object beneath prefix. Callers pass a "/"-terminated
	// prefix naming a logical directory (e.g. "chunks/"); under that convention
	// the filesystem and S3 backends enumerate identically.
	list(prefix string) ([]objectInfo, error)
}

// newBackend builds the backend for a vault location: an "s3://bucket/prefix"
// URL selects the S3 backend; anything else is a local filesystem path.
func newBackend(location string) (backend, error) {
	if strings.HasPrefix(location, "s3://") {
		return newS3Backend(location)
	}
	return &localBackend{root: location}, nil
}

// localBackend stores each object as a file beneath a directory.
type localBackend struct {
	root string
}

func (b *localBackend) path(key string) string {
	return filepath.Join(b.root, filepath.FromSlash(key))
}

func (b *localBackend) open(create bool) error {
	if !create {
		if _, err := os.Stat(b.root); err != nil {
			return fmt.Errorf("vault %q: %w", b.root, err)
		}
		return nil
	}
	// Create the root and the two top-level directories so a fresh vault is
	// immediately consistent (writes create their own shard subdirectories).
	for _, sub := range []string{"", "chunks", "snapshots"} {
		if err := os.MkdirAll(filepath.Join(b.root, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (b *localBackend) get(key string) ([]byte, error) {
	data, err := os.ReadFile(b.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", key, errNotFound)
	}
	return data, err
}

func (b *localBackend) put(key string, data []byte) error {
	dst := b.path(key)
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Write to a uniquely-named temp file and rename it into place, so a reader
	// never sees a partial object and two concurrent writers can't clobber one
	// another's temp. The "*.tmp" suffix keeps it out of list() results.
	tmp, err := os.CreateTemp(dir, "*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // harmless no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}

func (b *localBackend) exists(key string) (bool, error) {
	_, err := os.Stat(b.path(key))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (b *localBackend) list(prefix string) ([]objectInfo, error) {
	base := filepath.Join(b.root, filepath.FromSlash(prefix))
	if _, err := os.Stat(base); errors.Is(err, os.ErrNotExist) {
		return nil, nil // an absent prefix is simply empty
	}
	var out []objectInfo
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasSuffix(p, ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(b.root, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, objectInfo{key: filepath.ToSlash(rel), size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
