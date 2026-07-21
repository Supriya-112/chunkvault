package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatsCommand(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	vaultDir := t.TempDir()
	if _, err := runCmd("backup", src, "--vault", vaultDir); err != nil {
		t.Fatalf("backup: %v", err)
	}

	out, err := runCmd("stats", "--vault", vaultDir)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.Contains(out, "snapshots:") || !strings.Contains(out, "deduplicated") {
		t.Fatalf("unexpected stats output:\n%s", out)
	}
}
