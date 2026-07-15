package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// runCmd executes the root command with the given args, capturing output,
// and returns any error. It resets output routing so tests don't leak to stderr.
func runCmd(args ...string) (string, error) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestBackupRequiresExactlyOneArg(t *testing.T) {
	if _, err := runCmd("backup"); err == nil {
		t.Fatal("expected an error when 'backup' is called with no source dir")
	}
}

func TestRestoreRequiresTwoArgs(t *testing.T) {
	if _, err := runCmd("restore", "only-one"); err == nil {
		t.Fatal("expected an error when 'restore' is called with one arg")
	}
}

func TestBackupOnValidDir(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	vaultDir := t.TempDir()

	out, err := runCmd("backup", src, "--vault", vaultDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains([]byte(out), []byte("snapshot")) {
		t.Fatalf("expected a snapshot summary, got: %q", out)
	}
}
