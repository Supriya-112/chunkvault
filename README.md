# chunkvault

[![CI](https://github.com/Supriya-112/chunkvault/actions/workflows/ci.yml/badge.svg)](https://github.com/Supriya-112/chunkvault/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Supriya-112/chunkvault)](https://goreportcard.com/report/github.com/Supriya-112/chunkvault)

A content-addressable, **deduplicating backup tool** written in Go.

`chunkvault` splits your files into variable-sized chunks, fingerprints each one,
and stores only the chunks it hasn't seen before. Back up the same folder twice
and the second run stores almost nothing — only what actually changed. It's a
small, readable take on tools like [restic](https://restic.net) and
[borg](https://www.borgbackup.org), built to explore content-defined chunking,
deduplication, compression, encryption, and concurrent I/O.

> **Status: early development.** Building in the open, one milestone at a time —
> see the [roadmap](#roadmap).

## Why it exists

Naïve backups copy everything, every time. `chunkvault` demonstrates the ideas
behind modern deduplicating backup systems:

- **Content-defined chunking** — chunk boundaries follow the data (via a rolling
  hash), so inserting a byte doesn't shift every chunk after it.
- **Content addressing** — each chunk is stored under the hash of its contents,
  so identical chunks are automatically deduplicated.
- **Concurrency** — chunk hashing and writing run across a worker pool with
  proper cancellation.
- **Incremental** — backing up a source again reuses files unchanged since the
  last snapshot (by size and mtime), so only new and modified data is read.
- **Compression** — chunks are compressed before they're stored (zstd by
  default, gzip optional). Already-compressed data is detected and stored as-is,
  so a chunk never grows on disk.
- **Encryption at rest** — an optional passphrase (Argon2id → XChaCha20-Poly1305)
  encrypts every chunk and manifest, and names chunks by a keyed HMAC so even the
  file names on disk reveal nothing about their contents.

## Install

```bash
go install github.com/Supriya-112/chunkvault@latest
```

Or build from source:

```bash
git clone https://github.com/Supriya-112/chunkvault
cd chunkvault
go build -o chunkvault .
```

## Usage

```bash
# Back up a directory into the vault (chunks are hashed and stored in parallel;
# --workers defaults to one per CPU, and Ctrl-C stops the run cleanly)
chunkvault backup ./my-documents
chunkvault backup ./my-documents --workers 4

# Choose chunk compression: zstd (default), gzip, or none
chunkvault backup ./my-documents --compress gzip
chunkvault backup ./my-documents --compress none

# Create an encrypted vault (prompts for a passphrase the first time).
# The passphrase can also come from $CHUNKVAULT_PASSPHRASE or --passphrase-file.
chunkvault backup ./my-documents --vault ./secure --encrypt

# Once a vault is encrypted, backup/restore/stats detect that and ask for the
# passphrase automatically — no need to repeat --encrypt.
export CHUNKVAULT_PASSPHRASE=...        # for scripts and CI
chunkvault restore <snapshot-id> ./restored --vault ./secure

# Restore a snapshot into a target directory
chunkvault restore <snapshot-id> ./restored

# Check the vault's integrity: read every chunk back and confirm it still
# matches its ID (add a snapshot ID to check just one; --quick skips the
# content read and only confirms chunks are present)
chunkvault verify
chunkvault verify <snapshot-id>
chunkvault verify --quick

# Show deduplication statistics for the vault
chunkvault stats

# Show version
chunkvault --version
```

## How it works

```
files ──▶ chunker ──▶ [chunk, chunk, chunk] ──▶ hash each ──▶ compress + store unique only
                                                                  │
snapshot = ordered list of chunk hashes per file  ◀──────────────┘
```

Each chunk is hashed over its *uncompressed* bytes, so compression never
changes what deduplicates. A one-byte codec tag on every stored chunk records
how it was compressed, so a vault can mix codecs and restore always knows how to
decode. Restoring walks the snapshot's chunk list, pulls each chunk from the
store by its ID, decompresses it, and streams the file back out — verifying
integrity as it goes. The `verify` command runs that same check across every
stored chunk, and confirms each snapshot's chunks are present, without writing
anything — so you can catch bit-rot before you actually need to restore.

## Encryption

Pass `--encrypt` when first creating a vault and every chunk and manifest is
encrypted at rest:

- **Key derivation** — your passphrase is stretched with **Argon2id** (salt and
  parameters stored in `<vault>/config.json`) into a master key, from which an
  encryption key and a naming key are derived via HKDF.
- **Encryption** — chunks are compressed and then sealed with
  **XChaCha20-Poly1305**; the 24-byte random nonce makes per-chunk encryption
  safe without nonce bookkeeping. Snapshot manifests are encrypted the same way,
  so file names, sizes, and the directory tree are not exposed.
- **Content addressing without leaks** — an encrypted vault names each chunk by
  `HMAC-SHA256(plaintext)` under the naming key rather than the plaintext's raw
  SHA-256. Deduplication still works within the vault, but the file names reveal
  nothing about the contents and can't be used to test for a known file.
- **Integrity** — decryption is authenticated, and the recovered bytes are
  checked against the ID they were stored under, so tampering is caught.

**What it does not hide:** the number of chunks, their approximate sizes, and
the timing of snapshots are still observable from the vault directory. There is
no key rotation or passphrase change yet, and security rests on the strength of
your passphrase. Lose the passphrase and the data is unrecoverable — by design.

## Benchmarks

Representative throughput on an Intel Core i5-1038NG7 (4 cores), macOS, Go 1.26.
Run them yourself with `go test -bench=. -benchmem ./...` — numbers vary by
machine and disk.

| Benchmark            | Throughput       | Measures                                                     |
| -------------------- | ---------------- | ------------------------------------------------------------ |
| `Split`              | ~1.2 GB/s        | content-defined chunking (rolling hash + boundary search)    |
| `BackupFull`         | ~0.7 GB/s        | full backup: read + chunk + SHA-256 + store, across workers  |
| `BackupIncremental`  | ~20 GB/s (eff.)  | re-backup of unchanged data — every file reused, none re-read |
| `BackupZstd`         | ~180 MB/s        | full backup of compressible data, compressed with zstd (default) |
| `BackupGzip`         | ~120 MB/s        | full backup of compressible data, compressed with gzip       |

The incremental figure is effective throughput: it reflects skipping unchanged
data, not raw I/O. On the synthetic compressible dataset the compression
benchmarks shrink the vault ~1.6× (zstd) to ~1.9× (gzip); real-world text and
logs typically compress further. zstd is the default because it is roughly 50%
faster and closes most of that gap on real data. Compression trades throughput
for space — pass `--compress none` when the source is already compressed
(`BackupFull` above is the uncompressed path).

## Limitations

- **Regular files and directories only.** Directories (including empty ones) and
  their permissions are preserved, but symlinks, devices, sockets, and other
  non-regular files are skipped. Each `backup` run reports how many it skipped.
  Storing symlinks is planned for a later milestone.
- **Incremental backups use size + mtime.** Backing up a source again reuses
  files whose size and modification time match the previous snapshot, skipping
  the re-read. A file edited in place without its mtime changing (rare) would
  not be picked up.

## Roadmap

- [x] **M0** Project scaffold, CLI skeleton, CI
- [x] **M1** `backup`: walk files, chunk, hash, write to store (with basic dedup)
- [x] **M2** `restore`: reassemble files, verify chunk integrity, restore permissions
- [x] **M3** Backup → restore round-trip integration test
- [x] **M4** Content-defined chunking (rolling hash)
- [x] **M5** Deduplication + `stats` (dedup ratio, space saved)
- [x] **M6** Concurrent worker pool + cancellation
- [x] **M7** Incremental snapshots
- [x] **M8** Benchmarks
- [x] **M9** Compression (per-chunk zstd/gzip, incompressible fallback)
- [x] **M10** Encryption at rest (Argon2id + XChaCha20-Poly1305, keyed-HMAC names)
- [x] **M11** `verify` (whole-vault or per-snapshot integrity check, concurrent)
- [ ] **M12** TUI progress view
- [ ] **M13** Remote (S3) backend

## License

[MIT](LICENSE) © Supriya Patel
