package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBackupRestoreEncryptedCLI drives an encrypted backup -> stats -> restore
// through the CLI, supplying the passphrase via the environment.
func TestBackupRestoreEncryptedCLI(t *testing.T) {
	t.Setenv("CHUNKVAULT_PASSPHRASE", "cli-secret")

	src := t.TempDir()
	want := bytes.Repeat([]byte("secret cli data "), 3000)
	if err := os.WriteFile(filepath.Join(src, "a.txt"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	vaultDir := t.TempDir()

	out, err := runCmd("backup", src, "--vault", vaultDir, "--encrypt")
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if !strings.Contains(out, "encrypted: yes") {
		t.Fatalf("expected an encrypted marker in output:\n%s", out)
	}
	snapID := parseSnapshotID(t, out)

	if _, err := runCmd("stats", "--vault", vaultDir); err != nil {
		t.Fatalf("stats on encrypted vault: %v", err)
	}

	target := t.TempDir()
	if _, err := runCmd("restore", snapID, target, "--vault", vaultDir); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("restored content differs from source")
	}
}

// TestBackupEncryptEmptyPassphraseRejected guards the regression where an empty
// CHUNKVAULT_PASSPHRASE made `backup --encrypt` silently create a plaintext
// vault instead of erroring.
func TestBackupEncryptEmptyPassphraseRejected(t *testing.T) {
	t.Setenv("CHUNKVAULT_PASSPHRASE", "")
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runCmd("backup", src, "--vault", t.TempDir(), "--encrypt"); err == nil {
		t.Fatal("expected --encrypt with an empty passphrase to fail, not create a plaintext vault")
	}
}

func TestRestoreEncryptedWrongPassphraseCLI(t *testing.T) {
	t.Setenv("CHUNKVAULT_PASSPHRASE", "right-one")

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	vaultDir := t.TempDir()
	out, err := runCmd("backup", src, "--vault", vaultDir, "--encrypt")
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	snapID := parseSnapshotID(t, out)

	t.Setenv("CHUNKVAULT_PASSPHRASE", "wrong-one")
	if _, err := runCmd("restore", snapID, t.TempDir(), "--vault", vaultDir); err == nil {
		t.Fatal("expected restore with the wrong passphrase to fail")
	}
}
