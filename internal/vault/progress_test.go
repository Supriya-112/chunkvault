package vault

import (
	"context"
	"testing"
)

func TestBackupReportsProgress(t *testing.T) {
	files := map[string][]byte{
		"a.bin":     pseudoRandom(1, 300_000),
		"sub/b.bin": pseudoRandom(2, 200_000),
	}
	var total int64
	for _, d := range files {
		total += int64(len(d))
	}
	src := t.TempDir()
	writeTree(t, src, files)

	prog := NewProgress()
	if _, err := Backup(context.Background(), src, t.TempDir(), 4096, 4, Options{Compression: Zstd, Progress: prog}); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	s := prog.Snapshot()
	if s.Files != int64(len(files)) {
		t.Errorf("files = %d, want %d", s.Files, len(files))
	}
	if s.Total != total {
		t.Errorf("total = %d, want %d", s.Total, total)
	}
	if s.Done != total {
		t.Errorf("done = %d, want %d (bar should reach the total)", s.Done, total)
	}
	if s.Chunks == 0 {
		t.Error("expected some chunks to be processed")
	}
	if s.Fraction() != 1 {
		t.Errorf("fraction = %v, want 1", s.Fraction())
	}
}

func TestVerifyReportsProgress(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{"a.bin": pseudoRandom(1, 300_000)})
	vaultDir := t.TempDir()
	if _, err := Backup(context.Background(), src, vaultDir, 4096, 4, Options{Compression: Zstd}); err != nil {
		t.Fatal(err)
	}

	prog := NewProgress()
	rep, err := Verify(context.Background(), vaultDir, "", nil, 4, false, prog)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	s := prog.Snapshot()
	if s.Total == 0 || s.Done != s.Total {
		t.Errorf("verify progress incomplete: %+v", s)
	}
	if int(s.Total) != rep.Chunks {
		t.Errorf("progress total %d != report chunks %d", s.Total, rep.Chunks)
	}
	if s.Corrupt != 0 {
		t.Errorf("healthy vault reported %d corrupt", s.Corrupt)
	}
}

// TestNilProgressIsNoOp confirms the engine runs fine with progress tracking
// off (the common path).
func TestNilProgressIsNoOp(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{"a.txt": []byte("hi")})
	vaultDir := t.TempDir()
	if _, err := Backup(context.Background(), src, vaultDir, 4096, 4, Options{Compression: Zstd}); err != nil {
		t.Fatalf("Backup with nil progress: %v", err)
	}
	if _, err := Verify(context.Background(), vaultDir, "", nil, 4, false, nil); err != nil {
		t.Fatalf("Verify with nil progress: %v", err)
	}
}
