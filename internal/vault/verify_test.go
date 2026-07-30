package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Supriya-112/chunkvault/internal/chunk"
)

// backupOneFile writes a single file and backs it up, returning the vault dir
// and the snapshot ID.
func backupOneFile(t *testing.T, content []byte, opts Options) (vaultDir, snapID string) {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "f.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	vaultDir = t.TempDir()
	res, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 2, opts)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	return vaultDir, res.SnapshotID
}

func TestVerifyHealthyVault(t *testing.T) {
	vaultDir, _ := backupOneFile(t, []byte("healthy contents"), Options{Compression: Zstd})

	rep, err := Verify(context.Background(), vaultDir, "", nil, 4, false, nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("healthy vault reported problems: %+v", rep)
	}
	if rep.Chunks == 0 || rep.Snapshots != 1 {
		t.Fatalf("unexpected counts: chunks=%d snapshots=%d", rep.Chunks, rep.Snapshots)
	}
}

func TestVerifyDetectsCorruptChunk(t *testing.T) {
	vaultDir, snapID := backupOneFile(t, []byte("important data"), Options{Compression: Zstd})

	if err := os.WriteFile(firstChunkFile(t, vaultDir), []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Verify(context.Background(), vaultDir, "", nil, 4, false, nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if rep.OK() {
		t.Fatal("expected verify to flag the corrupt chunk")
	}
	if len(rep.Corrupt) != 1 {
		t.Fatalf("expected 1 corrupt chunk, got %d (%v)", len(rep.Corrupt), rep.Corrupt)
	}
	if len(rep.Broken) != 1 || rep.Broken[0] != snapID {
		t.Fatalf("expected snapshot %s to be reported broken, got %v", snapID, rep.Broken)
	}
}

func TestVerifyDetectsMissingChunk(t *testing.T) {
	vaultDir, snapID := backupOneFile(t, []byte("important data"), Options{Compression: NoCompression})

	if err := os.Remove(firstChunkFile(t, vaultDir)); err != nil {
		t.Fatal(err)
	}

	rep, err := Verify(context.Background(), vaultDir, "", nil, 4, false, nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if rep.OK() || len(rep.Missing) == 0 {
		t.Fatalf("expected verify to flag a missing chunk, got %+v", rep)
	}
	if len(rep.Broken) != 1 || rep.Broken[0] != snapID {
		t.Fatalf("expected snapshot %s broken, got %v", snapID, rep.Broken)
	}
}

// TestVerifyQuickSkipsContent shows the difference between the modes: a quick
// check passes a present-but-corrupt chunk, while a deep check catches it.
func TestVerifyQuickSkipsContent(t *testing.T) {
	vaultDir, _ := backupOneFile(t, []byte("important data"), Options{Compression: Zstd})
	if err := os.WriteFile(firstChunkFile(t, vaultDir), []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}

	quick, err := Verify(context.Background(), vaultDir, "", nil, 4, true, nil)
	if err != nil {
		t.Fatalf("quick Verify: %v", err)
	}
	if !quick.OK() {
		t.Fatalf("quick verify should not read contents, so it should pass: %+v", quick)
	}

	deep, err := Verify(context.Background(), vaultDir, "", nil, 4, false, nil)
	if err != nil {
		t.Fatalf("deep Verify: %v", err)
	}
	if deep.OK() {
		t.Fatal("deep verify should catch the corrupt chunk")
	}
}

func TestVerifySingleSnapshot(t *testing.T) {
	vaultDir, snapID := backupOneFile(t, []byte("some snapshot data"), Options{Compression: Zstd})

	rep, err := Verify(context.Background(), vaultDir, snapID, nil, 4, false, nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !rep.OK() || rep.Snapshots != 1 {
		t.Fatalf("single-snapshot verify: %+v", rep)
	}

	if _, err := Verify(context.Background(), vaultDir, "no-such-snapshot", nil, 4, false, nil); err == nil {
		t.Fatal("verifying a nonexistent snapshot should error")
	}
}

func TestVerifyEncryptedVault(t *testing.T) {
	vaultDir, _ := backupOneFile(t, []byte("secret contents"), Options{Compression: Zstd, Passphrase: testPass})

	rep, err := Verify(context.Background(), vaultDir, "", testPass, 4, false, nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("healthy encrypted vault reported problems: %+v", rep)
	}

	// Without the passphrase, an encrypted vault cannot be verified.
	if _, err := Verify(context.Background(), vaultDir, "", nil, 4, false, nil); !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("got %v, want ErrPassphraseRequired", err)
	}

	// Tampering is caught even through the encryption layer.
	if err := os.WriteFile(firstChunkFile(t, vaultDir), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err = Verify(context.Background(), vaultDir, "", testPass, 4, false, nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if rep.OK() || len(rep.Corrupt) != 1 {
		t.Fatalf("expected one corrupt chunk in encrypted vault, got %+v", rep)
	}
}
