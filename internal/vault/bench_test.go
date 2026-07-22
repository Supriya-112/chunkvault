package vault

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Supriya-112/chunkvault/internal/chunk"
)

// writeBenchDataset writes a fixed synthetic dataset under dir and returns its
// total size in bytes.
func writeBenchDataset(b *testing.B, dir string) int64 {
	b.Helper()
	var total int64
	for i := 0; i < 4; i++ {
		data := pseudoRandom(uint64(i+1), 8<<20) // 8 MiB each -> 32 MiB total
		p := filepath.Join(dir, "file"+strconv.Itoa(i)+".bin")
		if err := os.WriteFile(p, data, 0o644); err != nil {
			b.Fatal(err)
		}
		total += int64(len(data))
	}
	return total
}

// BenchmarkBackupFull measures a from-scratch backup: every file is read,
// chunked, hashed, and stored across the worker pool.
func BenchmarkBackupFull(b *testing.B) {
	src := b.TempDir()
	total := writeBenchDataset(b, src)
	b.SetBytes(total)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		vaultDir := b.TempDir() // fresh vault so every run is a full backup
		b.StartTimer()
		if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 0); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBackupIncremental measures re-backing up unchanged data: every file
// is reused from the parent snapshot, so nothing is re-read or re-hashed.
func BenchmarkBackupIncremental(b *testing.B) {
	src := b.TempDir()
	total := writeBenchDataset(b, src)
	vaultDir := b.TempDir()
	if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 0); err != nil {
		b.Fatal(err) // prime the parent snapshot
	}
	b.SetBytes(total)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 0); err != nil {
			b.Fatal(err)
		}
	}
}
