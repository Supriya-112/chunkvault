package vault

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Supriya-112/chunkvault/internal/chunk"
)

var testPass = []byte("correct horse battery staple")

// writeTree writes rel-path→contents under root, creating parent dirs.
func writeTree(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for rel, data := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEncryptedBackupRestoreRoundTrip(t *testing.T) {
	files := map[string][]byte{
		"notes.txt":    bytes.Repeat([]byte("top secret memo "), 4000),
		"sub/data.bin": pseudoRandom(5, 200_000),
	}
	src := t.TempDir()
	writeTree(t, src, files)

	vaultDir := t.TempDir()
	res, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 4, Options{Compression: Zstd, Passphrase: testPass})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if enc, err := IsEncrypted(vaultDir); err != nil || !enc {
		t.Fatalf("IsEncrypted = %v, %v; want true, nil", enc, err)
	}

	target := t.TempDir()
	if _, err := Restore(vaultDir, res.SnapshotID, target, testPass); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(target, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: content mismatch after encrypted round-trip", rel)
		}
	}
}

func TestEncryptedRestoreWrongPassphrase(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{"a.txt": []byte("secret")})
	vaultDir := t.TempDir()
	res, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 2, Options{Passphrase: testPass})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(vaultDir, res.SnapshotID, t.TempDir(), []byte("not it")); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("got %v, want ErrWrongPassphrase", err)
	}
}

func TestEncryptedVaultRequiresPassphrase(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{"a.txt": []byte("secret")})
	vaultDir := t.TempDir()
	res, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 2, Options{Passphrase: testPass})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(vaultDir, res.SnapshotID, t.TempDir(), nil); !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("restore: got %v, want ErrPassphraseRequired", err)
	}
	if _, err := ComputeStats(vaultDir, nil); !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("stats: got %v, want ErrPassphraseRequired", err)
	}
	if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 2, Options{}); !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("backup: got %v, want ErrPassphraseRequired", err)
	}
}

// TestPassphraseRejectedForPlainVault checks a passphrase is refused (not
// silently ignored) for a vault that already holds unencrypted data.
func TestPassphraseRejectedForPlainVault(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{"a.txt": []byte("hello")})
	vaultDir := t.TempDir()
	if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 2, Options{}); err != nil {
		t.Fatalf("first (plain) backup: %v", err)
	}
	if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 2, Options{Passphrase: testPass}); !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("got %v, want ErrNotEncrypted", err)
	}
}

// TestEncryptedNothingStoredInPlaintext is the core at-rest guarantee: with
// compression off (so plaintext would otherwise be verbatim on disk), neither
// the chunk files nor the manifest may contain the secret bytes or the file
// name.
func TestEncryptedNothingStoredInPlaintext(t *testing.T) {
	marker := []byte("MARKER-eF9k2p-topsecret")
	secretName := "confidential-report.txt"
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{secretName: bytes.Repeat(marker, 2000)})

	vaultDir := t.TempDir()
	if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 2, Options{Compression: NoCompression, Passphrase: testPass}); err != nil {
		t.Fatal(err)
	}

	scan := func(dir string) {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if bytes.Contains(data, marker) {
				t.Errorf("plaintext marker leaked in %s", p)
			}
			if bytes.Contains(data, []byte(secretName)) {
				t.Errorf("source file name leaked in %s", p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	scan(filepath.Join(vaultDir, "chunks"))
	scan(filepath.Join(vaultDir, "snapshots"))
}

func TestEncryptedIncremental(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{
		"stable.bin":   pseudoRandom(1, 200_000),
		"changing.bin": pseudoRandom(2, 200_000),
	})
	vaultDir := t.TempDir()
	if _, err := Backup(context.Background(), src, vaultDir, 4096, 4, Options{Passphrase: testPass}); err != nil {
		t.Fatalf("first backup: %v", err)
	}

	if err := os.WriteFile(filepath.Join(src, "changing.bin"), pseudoRandom(3, 200_000), 0o644); err != nil {
		t.Fatal(err)
	}
	bumped := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(src, "changing.bin"), bumped, bumped); err != nil {
		t.Fatal(err)
	}

	second, err := Backup(context.Background(), src, vaultDir, 4096, 4, Options{Passphrase: testPass})
	if err != nil {
		t.Fatalf("second backup: %v", err)
	}
	if second.Reused != 1 {
		t.Fatalf("expected 1 reused file, got %d", second.Reused)
	}

	target := t.TempDir()
	if _, err := Restore(vaultDir, second.SnapshotID, target, testPass); err != nil {
		t.Fatalf("restore: %v", err)
	}
	for _, name := range []string{"stable.bin", "changing.bin"} {
		got, _ := os.ReadFile(filepath.Join(target, name))
		want, _ := os.ReadFile(filepath.Join(src, name))
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: restored content differs from source", name)
		}
	}
}

func TestEncryptedStats(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{"a.log": bytes.Repeat([]byte("log line\n"), 50_000)})
	vaultDir := t.TempDir()
	if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 4, Options{Compression: Zstd, Passphrase: testPass}); err != nil {
		t.Fatal(err)
	}
	st, err := ComputeStats(vaultDir, testPass)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Snapshots != 1 {
		t.Fatalf("snapshots = %d, want 1", st.Snapshots)
	}
	if st.LogicalBytes == 0 {
		t.Fatal("expected non-zero logical bytes read from the manifest")
	}
}
