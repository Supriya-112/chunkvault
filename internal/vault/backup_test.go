package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// pseudoRandom returns n deterministic, well-mixed bytes so tests exercise
// many-chunk files without depending on a random seed.
func pseudoRandom(seed uint64, n int) []byte {
	out := make([]byte, n)
	x := seed
	for i := range out {
		x += 0x9e3779b97f4a7c15
		z := x
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		out[i] = byte(z >> 24)
	}
	return out
}

// TestBackupPreservesChunkOrderAcrossWorkers backs up multi-chunk files with a
// pool of workers and restores them, which only round-trips correctly if each
// file's chunks are reassembled in their original order regardless of the order
// workers finish in. A small chunk size forces many chunks per file.
func TestBackupPreservesChunkOrderAcrossWorkers(t *testing.T) {
	src := t.TempDir()
	files := map[string][]byte{
		"a.bin":      pseudoRandom(1, 300_000),
		"b.bin":      pseudoRandom(2, 250_000),
		"sub/c.bin":  pseudoRandom(3, 400_000),
		"sub/deep/d": pseudoRandom(4, 128_000),
	}
	for rel, data := range files {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	vaultDir := t.TempDir()
	const smallChunk = 1024
	res, err := Backup(context.Background(), src, vaultDir, smallChunk, 8)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if res.Files != len(files) {
		t.Fatalf("backed up %d files, want %d", res.Files, len(files))
	}

	target := t.TempDir()
	if _, err := Restore(vaultDir, res.SnapshotID, target); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(target, rel))
		if err != nil {
			t.Fatalf("reading restored %s: %v", rel, err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: restored %d bytes, want %d", rel, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: byte %d differs after round-trip; chunk order not preserved", rel, i)
			}
		}
	}
}

// TestBackupCancelled confirms a cancelled context aborts the run with
// context.Canceled instead of writing a snapshot.
func TestBackupCancelled(t *testing.T) {
	src := t.TempDir()
	for i := 0; i < 20; i++ {
		name := filepath.Join(src, fmt.Sprintf("f%02d.bin", i))
		if err := os.WriteFile(name, pseudoRandom(uint64(i+1), 100_000), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the run starts

	_, err := Backup(ctx, src, t.TempDir(), 1024, 4)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// TestBackupStoreWriteErrorAborts makes chunk writes fail mid-run and asserts
// the run aborts with an error and writes no snapshot. This exercises the
// worker-error path: a failing PutChunk cancels the run and propagates the error
// back through the collector.
func TestBackupStoreWriteErrorAborts(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses filesystem permissions")
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "f.bin"), pseudoRandom(7, 200_000), 0o644); err != nil {
		t.Fatal(err)
	}
	vaultDir := t.TempDir()
	if _, err := Open(vaultDir); err != nil { // create chunks/ and snapshots/
		t.Fatal(err)
	}
	chunksDir := filepath.Join(vaultDir, "chunks")
	if err := os.Chmod(chunksDir, 0o555); err != nil { // read-only: writes fail
		t.Fatal(err)
	}
	defer os.Chmod(chunksDir, 0o755) // restore so t.TempDir cleanup can remove it

	if _, err := Backup(context.Background(), src, vaultDir, 4096, 4); err == nil {
		t.Fatal("expected an error when the chunk store is not writable")
	}
	entries, err := os.ReadDir(filepath.Join(vaultDir, "snapshots"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("no snapshot should be written on failure, found %d", len(entries))
	}
}

// TestBackupUnreadableSourceFile asserts an unreadable source file surfaces an
// error rather than being silently skipped.
func TestBackupUnreadableSourceFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses filesystem permissions")
	}
	src := t.TempDir()
	p := filepath.Join(src, "secret.bin")
	if err := os.WriteFile(p, []byte("classified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(p, 0o644)

	if _, err := Backup(context.Background(), src, t.TempDir(), 4096, 4); err == nil {
		t.Fatal("expected an error backing up an unreadable file")
	}
}
