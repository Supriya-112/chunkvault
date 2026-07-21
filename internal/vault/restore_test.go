package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Supriya-112/chunkvault/internal/chunk"
)

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
