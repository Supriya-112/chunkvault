package cmd

import (
	"bytes"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRoundTripCLI runs `backup` then `restore` through the CLI and checks the
// restored tree is byte-for-byte identical to the source, covering nested
// directories, an empty file, a sub-chunk file, a multi-chunk file, and
// non-default file modes.
func TestRoundTripCLI(t *testing.T) {
	src := t.TempDir()

	files := []struct {
		rel  string
		data []byte
		mode os.FileMode
	}{
		{"README.md", []byte("# hello\n"), 0o644},
		{"empty", []byte{}, 0o644},
		{"bin/tool.sh", []byte("#!/bin/sh\necho hi\n"), 0o755},
		// > 1 MiB so it splits into multiple chunks; the repeating content also
		// means the interior chunks deduplicate against each other.
		{"data/big.bin", bytes.Repeat([]byte("abcd"), 700_000), 0o644},
		{"data/nested/note.txt", []byte("nested note\n"), 0o600},
	}
	for _, f := range files {
		p := filepath.Join(src, f.rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, f.data, f.mode); err != nil {
			t.Fatal(err)
		}
	}

	vaultDir := t.TempDir()
	out, err := runCmd("backup", src, "--vault", vaultDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	snapID := parseSnapshotID(t, out)

	target := t.TempDir()
	if _, err := runCmd("restore", snapID, target, "--vault", vaultDir); err != nil {
		t.Fatalf("restore: %v", err)
	}

	assertTreesEqual(t, src, target)
}

// TestSecondBackupIsIncremental checks that a second backup of unchanged data
// reuses files from the previous snapshot rather than re-reading them, storing
// no new chunks.
func TestSecondBackupIsIncremental(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), bytes.Repeat([]byte("x"), 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	vaultDir := t.TempDir()

	if _, err := runCmd("backup", src, "--vault", vaultDir); err != nil {
		t.Fatalf("first backup: %v", err)
	}
	out, err := runCmd("backup", src, "--vault", vaultDir)
	if err != nil {
		t.Fatalf("second backup: %v", err)
	}
	if !strings.Contains(out, "reused:") {
		t.Fatalf("expected the second backup to reuse unchanged files, got:\n%s", out)
	}
	if !strings.Contains(out, "0 new") {
		t.Fatalf("expected the second backup to store 0 new chunks, got:\n%s", out)
	}
}

// parseSnapshotID extracts the snapshot ID from the first line of backup output,
// which has the form "snapshot <id>".
func parseSnapshotID(t *testing.T, backupOutput string) string {
	t.Helper()
	line, _, _ := strings.Cut(backupOutput, "\n")
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "snapshot" {
		t.Fatalf("could not parse snapshot id from backup output:\n%s", backupOutput)
	}
	return fields[1]
}

type fileFingerprint struct {
	sum  [32]byte
	mode os.FileMode
}

// assertTreesEqual fails the test unless a and b contain exactly the same
// regular files with identical contents and permission bits.
func assertTreesEqual(t *testing.T, a, b string) {
	t.Helper()
	fa := treeFingerprint(t, a)
	fb := treeFingerprint(t, b)

	for rel, want := range fa {
		got, ok := fb[rel]
		if !ok {
			t.Errorf("file %q missing from restored tree", rel)
			continue
		}
		if want.sum != got.sum {
			t.Errorf("content mismatch for %q", rel)
		}
		if want.mode.Perm() != got.mode.Perm() {
			t.Errorf("mode mismatch for %q: source %v, restored %v", rel, want.mode.Perm(), got.mode.Perm())
		}
	}
	for rel := range fb {
		if _, ok := fa[rel]; !ok {
			t.Errorf("unexpected extra file %q in restored tree", rel)
		}
	}
}

// treeFingerprint maps each regular file under root (by path relative to root)
// to a hash of its contents and its mode.
func treeFingerprint(t *testing.T, root string) map[string]fileFingerprint {
	t.Helper()
	out := map[string]fileFingerprint{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[rel] = fileFingerprint{sum: sha256.Sum256(data), mode: info.Mode()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
