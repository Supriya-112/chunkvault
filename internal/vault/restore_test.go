package vault

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Supriya-112/chunkvault/internal/chunk"
)

// firstChunkFile returns the path of any one stored chunk file in the vault.
func firstChunkFile(t *testing.T, vaultDir string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(filepath.Join(vaultDir, "chunks"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && found == "" {
			found = p
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatal("no chunk file found in vault")
	}
	return found
}

func TestBackupRestoreRoundTrip(t *testing.T) {
	src := t.TempDir()
	want := []byte("the quick brown fox jumps over the lazy dog\n")
	if err := os.WriteFile(filepath.Join(src, "note.txt"), want, 0o644); err != nil {
		t.Fatal(err)
	}

	vaultDir := t.TempDir()
	backupRes, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 4)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	target := t.TempDir()
	if _, err := Restore(vaultDir, backupRes.SnapshotID, target); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(target, "note.txt"))
	if err != nil {
		t.Fatalf("reading restored file: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("restored content mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestBackupRestorePreservesEmptyDirs(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "outer/inner"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "outer/file.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	vaultDir := t.TempDir()
	res, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 4)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	target := t.TempDir()
	if _, err := Restore(vaultDir, res.SnapshotID, target); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	for _, dir := range []string{"empty", "outer", "outer/inner"} {
		info, err := os.Stat(filepath.Join(target, dir))
		if err != nil {
			t.Fatalf("restored dir %q missing: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%q was restored as a non-directory", dir)
		}
	}

	// Directory permissions should be preserved too.
	srcInfo, err := os.Stat(filepath.Join(src, "outer/inner"))
	if err != nil {
		t.Fatal(err)
	}
	gotInfo, err := os.Stat(filepath.Join(target, "outer/inner"))
	if err != nil {
		t.Fatal(err)
	}
	if srcInfo.Mode().Perm() != gotInfo.Mode().Perm() {
		t.Errorf("dir mode not preserved: source %v, restored %v", srcInfo.Mode().Perm(), gotInfo.Mode().Perm())
	}
}

func TestRestoreDetectsCorruptChunk(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("important data"), 0o644); err != nil {
		t.Fatal(err)
	}
	vaultDir := t.TempDir()
	res, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 2)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// Tamper with a stored chunk without changing its file name (its hash).
	if err := os.WriteFile(firstChunkFile(t, vaultDir), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = Restore(vaultDir, res.SnapshotID, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("expected an integrity error, got %v", err)
	}
}

func TestRestoreMissingChunk(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("important data"), 0o644); err != nil {
		t.Fatal(err)
	}
	vaultDir := t.TempDir()
	res, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 2)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	if err := os.Remove(firstChunkFile(t, vaultDir)); err != nil {
		t.Fatal(err)
	}

	if _, err := Restore(vaultDir, res.SnapshotID, t.TempDir()); err == nil {
		t.Fatal("expected an error restoring with a missing chunk")
	}
}

func TestRestoreVaultNotFound(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := Restore(missing, "snap", t.TempDir()); err == nil {
		t.Fatal("expected an error restoring from a nonexistent vault")
	}
}

func TestSafeJoinRejectsEscape(t *testing.T) {
	for _, bad := range []string{"../evil", "../../etc/passwd", "/abs/path"} {
		if _, err := safeJoin("/tmp/target", bad); err == nil {
			t.Errorf("safeJoin allowed unsafe path %q", bad)
		}
	}
	if _, err := safeJoin("/tmp/target", "sub/ok.txt"); err != nil {
		t.Errorf("safeJoin rejected a safe path: %v", err)
	}
}
