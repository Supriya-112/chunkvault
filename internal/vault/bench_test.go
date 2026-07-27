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
// total size in bytes. The data is well-mixed (incompressible), so it measures
// raw chunk/hash/store throughput without any compression benefit.
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

// writeCompressibleDataset writes a dataset with roughly four bits of entropy
// per byte (a 16-symbol alphabet), so compression has real work to do while the
// chunks still differ from one another and do not simply deduplicate away.
func writeCompressibleDataset(b *testing.B, dir string) int64 {
	b.Helper()
	var total int64
	for i := 0; i < 4; i++ {
		data := pseudoRandom(uint64(i+1), 8<<20)
		for j := range data {
			data[j] = 'a' + (data[j] & 0x0f)
		}
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
		if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 0, NoCompression); err != nil {
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
	if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 0, NoCompression); err != nil {
		b.Fatal(err) // prime the parent snapshot
	}
	b.SetBytes(total)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 0, NoCompression); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBackupZstd and BenchmarkBackupGzip measure a full backup of
// compressible data with each codec, reporting throughput plus the on-disk
// size reduction (an "x-smaller" custom metric).
func BenchmarkBackupZstd(b *testing.B) { benchmarkCompressedBackup(b, Zstd) }
func BenchmarkBackupGzip(b *testing.B) { benchmarkCompressedBackup(b, Gzip) }

func benchmarkCompressedBackup(b *testing.B, codec Codec) {
	src := b.TempDir()
	total := writeCompressibleDataset(b, src)

	// Measure the compression ratio once, outside the timed loop.
	measure := b.TempDir()
	if _, err := Backup(context.Background(), src, measure, chunk.DefaultSize, 0, codec); err != nil {
		b.Fatal(err)
	}
	st, err := ComputeStats(measure)
	if err != nil {
		b.Fatal(err)
	}
	ratio := float64(total) / float64(st.StoredBytes)

	b.SetBytes(total)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		vaultDir := b.TempDir()
		b.StartTimer()
		if _, err := Backup(context.Background(), src, vaultDir, chunk.DefaultSize, 0, codec); err != nil {
			b.Fatal(err)
		}
	}

	// Reported after the loop: ResetTimer discards custom metrics set before it.
	b.ReportMetric(ratio, "x-smaller")
}
