package vault

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Supriya-112/chunkvault/internal/chunk"
)

func TestComputeStatsReportsDedup(t *testing.T) {
	src := t.TempDir()
	// Two identical files: the second one's chunks all deduplicate against the
	// first, so stored bytes should be roughly half the logical bytes.
	content := bytes.Repeat([]byte("chunkvault stats test\n"), 100_000)
	for _, name := range []string{"a.log", "b.log"} {
		if err := os.WriteFile(filepath.Join(src, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	vaultDir := t.TempDir()
	if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 4); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	st, err := ComputeStats(vaultDir)
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}

	if st.Snapshots != 1 {
		t.Errorf("snapshots = %d, want 1", st.Snapshots)
	}
	if want := int64(2 * len(content)); st.LogicalBytes != want {
		t.Errorf("logical bytes = %d, want %d", st.LogicalBytes, want)
	}
	if st.ChunkRefs <= st.UniqueChunks {
		t.Errorf("with duplicate files, chunk references (%d) should exceed unique chunks (%d)", st.ChunkRefs, st.UniqueChunks)
	}
	if st.SavedBytes() <= 0 || st.DedupRatio() <= 0 {
		t.Errorf("expected positive dedup, got saved=%d ratio=%.2f", st.SavedBytes(), st.DedupRatio())
	}
}

func TestComputeStatsEmptyVault(t *testing.T) {
	st, err := ComputeStats(t.TempDir())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if st.Snapshots != 0 || st.UniqueChunks != 0 || st.LogicalBytes != 0 {
		t.Errorf("expected empty stats, got %+v", *st)
	}
	if st.DedupRatio() != 0 {
		t.Errorf("dedup ratio of an empty vault = %v, want 0", st.DedupRatio())
	}
}
