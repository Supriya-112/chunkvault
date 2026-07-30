package vault

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Supriya-112/chunkvault/internal/chunk"
)

// TestS3RoundTrip runs a real backup -> verify -> restore against an
// S3-compatible endpoint. It is skipped unless CHUNKVAULT_S3_TEST_URL is set, so
// CI (which has no credentials) stays green. To run it against a local MinIO:
//
//	CHUNKVAULT_S3_TEST_URL=s3://mybucket/chunkvault-test \
//	AWS_ENDPOINT_URL=http://localhost:9000 AWS_REGION=us-east-1 \
//	AWS_ACCESS_KEY_ID=minioadmin AWS_SECRET_ACCESS_KEY=minioadmin \
//	go test ./internal/vault/ -run TestS3RoundTrip -v
func TestS3RoundTrip(t *testing.T) {
	loc := os.Getenv("CHUNKVAULT_S3_TEST_URL")
	if loc == "" {
		t.Skip("set CHUNKVAULT_S3_TEST_URL (and AWS_* env) to run the S3 integration test")
	}

	files := map[string][]byte{
		"a.txt":       []byte("hello from s3"),
		"sub/big.bin": pseudoRandom(1, 500_000),
	}
	src := t.TempDir()
	writeTree(t, src, files)

	res, err := Backup(context.Background(), src, loc, chunk.DefaultSize, 4, Options{Compression: Zstd})
	if err != nil {
		t.Fatalf("Backup to S3: %v", err)
	}

	rep, err := Verify(context.Background(), loc, "", nil, 4, false, nil)
	if err != nil {
		t.Fatalf("Verify on S3: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("S3 vault failed verification: %+v", rep)
	}

	dst := t.TempDir()
	if _, err := Restore(loc, res.SnapshotID, dst, nil); err != nil {
		t.Fatalf("Restore from S3: %v", err)
	}
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("reading restored %s: %v", rel, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: content differs after S3 round-trip", rel)
		}
	}
}
