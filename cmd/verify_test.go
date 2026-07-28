package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tamperAnyChunk overwrites one stored chunk file with garbage.
func tamperAnyChunk(t *testing.T, vaultDir string) {
	t.Helper()
	var target string
	err := filepath.WalkDir(filepath.Join(vaultDir, "chunks"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && target == "" {
			target = p
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if target == "" {
		t.Fatal("no chunk file to tamper with")
	}
	if err := os.WriteFile(target, []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyCLI(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("verify me please"), 0o644); err != nil {
		t.Fatal(err)
	}
	vaultDir := t.TempDir()
	if _, err := runCmd("backup", src, "--vault", vaultDir); err != nil {
		t.Fatalf("backup: %v", err)
	}

	out, err := runCmd("verify", "--vault", vaultDir)
	if err != nil {
		t.Fatalf("verify of a healthy vault: %v", err)
	}
	if !strings.Contains(out, "no problems found") {
		t.Fatalf("expected a clean report, got:\n%s", out)
	}

	// A corrupt chunk must make verify fail (non-nil error => non-zero exit).
	tamperAnyChunk(t, vaultDir)
	if _, err := runCmd("verify", "--vault", vaultDir); err == nil {
		t.Fatal("expected verify to fail on a corrupt vault")
	}

	// Quick mode only checks presence, so it still passes.
	if _, err := runCmd("verify", "--vault", vaultDir, "--quick"); err != nil {
		t.Fatalf("quick verify should pass on a present-but-corrupt chunk: %v", err)
	}
}
