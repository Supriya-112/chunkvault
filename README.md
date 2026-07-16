# chunkvault

[![CI](https://github.com/Supriya-112/chunkvault/actions/workflows/ci.yml/badge.svg)](https://github.com/Supriya-112/chunkvault/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Supriya-112/chunkvault)](https://goreportcard.com/report/github.com/Supriya-112/chunkvault)

A content-addressable, **deduplicating backup tool** written in Go.

`chunkvault` splits your files into variable-sized chunks, fingerprints each one,
and stores only the chunks it hasn't seen before. Back up the same folder twice
and the second run stores almost nothing — only what actually changed. It's a
small, readable take on tools like [restic](https://restic.net) and
[borg](https://www.borgbackup.org), built to explore content-defined chunking,
deduplication, and concurrent I/O.

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
# Back up a directory into the vault
chunkvault backup ./my-documents

# Restore a snapshot into a target directory
chunkvault restore <snapshot-id> ./restored

# Show version
chunkvault --version
```

## How it works

```
files ──▶ chunker ──▶ [chunk, chunk, chunk] ──▶ hash each ──▶ store unique only
                                                                  │
snapshot = ordered list of chunk hashes per file  ◀──────────────┘
```

Restoring walks the snapshot's chunk list, pulls each chunk from the store by
its hash, and streams the file back out — verifying integrity as it goes.

## Roadmap

- [x] **M0** Project scaffold, CLI skeleton, CI
- [x] **M1** `backup`: walk files, chunk, hash, write to store (with basic dedup)
- [x] **M2** `restore`: reassemble files, verify chunk integrity, restore permissions
- [ ] **M3** Backup → restore round-trip integration test
- [ ] **M4** Content-defined chunking (rolling hash)
- [ ] **M5** Deduplication + `stats` (dedup ratio, space saved)
- [ ] **M6** Concurrent worker pool + cancellation
- [ ] **M7** Incremental snapshots
- [ ] **M8** Benchmarks
- [ ] **M9** Compression
- [ ] **M10** Encryption at rest
- [ ] **M11** `verify` (corruption detection)
- [ ] **M12** TUI progress view
- [ ] **M13** Remote (S3) backend

## License

[MIT](LICENSE) © Supriya Patel
