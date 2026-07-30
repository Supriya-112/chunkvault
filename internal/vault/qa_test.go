package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Supriya-112/chunkvault/internal/chunk"
)

// failBackend fails every operation, to exercise error-propagation paths.
type failBackend struct{}

func (failBackend) open(bool) error                   { return nil }
func (failBackend) get(string) ([]byte, error)        { return nil, errors.New("backend down") }
func (failBackend) put(string, []byte) error          { return errors.New("backend down") }
func (failBackend) exists(string) (bool, error)       { return false, errors.New("backend down") }
func (failBackend) list(string) ([]objectInfo, error) { return nil, errors.New("backend down") }

// TestEstablishedFailsClosed: a list error must surface, not be swallowed into
// "not established" (which would let --encrypt clobber an existing vault).
func TestEstablishedFailsClosed(t *testing.T) {
	if _, err := established(failBackend{}); err == nil {
		t.Fatal("established should return the list error rather than reporting false")
	}
}

// TestPartitionByPresencePropagatesError: a transient exists error must not be
// misreported as a missing chunk.
func TestPartitionByPresencePropagatesError(t *testing.T) {
	s := &Store{backend: failBackend{}}
	if _, _, err := s.partitionByPresence([]string{"abc"}); err == nil {
		t.Fatal("partitionByPresence should propagate a backend error")
	}
}

// TestRestoreRestrictiveDirMode exercises the create-then-chmod-deepest-first
// ordering: a directory whose final mode is 0o500 must still receive the file
// stored inside it.
func TestRestoreRestrictiveDirMode(t *testing.T) {
	src := t.TempDir()
	sub := filepath.Join(src, "locked")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "secret.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(sub, 0o755) }) // let t.TempDir cleanup remove it

	vaultDir := t.TempDir()
	res, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 2, Options{})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	target := t.TempDir()
	if _, err := Restore(vaultDir, res.SnapshotID, target, nil); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	restoredDir := filepath.Join(target, "locked")
	t.Cleanup(func() { os.Chmod(restoredDir, 0o755) })

	got, err := os.ReadFile(filepath.Join(restoredDir, "secret.txt"))
	if err != nil {
		t.Fatalf("reading file inside a 0o500 dir: %v", err)
	}
	if string(got) != "data" {
		t.Fatalf("content mismatch: %q", got)
	}
	info, err := os.Stat(restoredDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o500 {
		t.Errorf("restored dir mode = %o, want 500", info.Mode().Perm())
	}
}

// TestRestoreRejectsUnsafeSnapshotPath: a hostile snapshot with a "../" path is
// rejected end-to-end (not just at the safeJoin unit level).
func TestRestoreRejectsUnsafeSnapshotPath(t *testing.T) {
	vaultDir := t.TempDir()
	store, err := Open(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	snap := &Snapshot{ID: "malicious", Source: "/x", Files: []FileEntry{{Path: "../escape.txt", Mode: 0o644}}}
	if err := store.SaveSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(vaultDir, "malicious", t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("expected an unsafe-path error, got %v", err)
	}
}

// TestSnapshotIDBindingDetectsSwap: a manifest served under a different name
// than its own ID is rejected, catching a swap/rollback of snapshot objects.
func TestSnapshotIDBindingDetectsSwap(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{"a.txt": []byte("data")})
	vaultDir := t.TempDir()
	res, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 2, Options{})
	if err != nil {
		t.Fatal(err)
	}

	orig := filepath.Join(vaultDir, "snapshots", res.SnapshotID+".json")
	data, err := os.ReadFile(orig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "snapshots", "forged.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(vaultDir, "forged", t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "mismatched id") {
		t.Fatalf("expected a mismatched-id error, got %v", err)
	}
}

// TestTwoSourcesNoCrossReuse: incremental reuse is scoped to the same source —
// backing up a different source into one vault reuses nothing.
func TestTwoSourcesNoCrossReuse(t *testing.T) {
	srcA := t.TempDir()
	writeTree(t, srcA, map[string][]byte{"a.txt": []byte("alpha content here")})
	srcB := t.TempDir()
	writeTree(t, srcB, map[string][]byte{"b.txt": []byte("beta content here")})
	vaultDir := t.TempDir()

	if _, err := Backup(context.Background(), srcA, vaultDir, chunk.DefaultSize, 2, Options{}); err != nil {
		t.Fatal(err)
	}
	resB, err := Backup(context.Background(), srcB, vaultDir, chunk.DefaultSize, 2, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if resB.Reused != 0 {
		t.Fatalf("a different source should reuse nothing, got %d", resB.Reused)
	}
	resB2, err := Backup(context.Background(), srcB, vaultDir, chunk.DefaultSize, 2, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if resB2.Reused != 1 {
		t.Fatalf("re-backing up the same source should reuse its file, got %d", resB2.Reused)
	}
}
