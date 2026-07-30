package vault

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// s3Backend stores objects in an S3 (or S3-compatible) bucket under an optional
// key prefix. Credentials come from the standard AWS environment; the endpoint
// and region can be overridden with AWS_ENDPOINT_URL and AWS_REGION for
// S3-compatible services such as MinIO, Cloudflare R2, or Backblaze B2.
type s3Backend struct {
	client *minio.Client
	bucket string
	prefix string // key prefix within the bucket ("" for the bucket root)
}

// newS3Backend builds the backend for an "s3://bucket/prefix" location.
func newS3Backend(rawURL string) (*s3Backend, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing vault URL %q: %w", rawURL, err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("s3 vault URL %q has no bucket", rawURL)
	}

	host, secure := "s3.amazonaws.com", true
	if ep := os.Getenv("AWS_ENDPOINT_URL"); ep != "" {
		if !strings.Contains(ep, "://") {
			ep = "https://" + ep
		}
		eu, err := url.Parse(ep)
		if err != nil {
			return nil, fmt.Errorf("parsing AWS_ENDPOINT_URL: %w", err)
		}
		host, secure = eu.Host, eu.Scheme != "http"
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	client, err := minio.New(host, &minio.Options{
		Creds: credentials.NewChainCredentials([]credentials.Provider{
			&credentials.EnvAWS{},
			&credentials.FileAWSCredentials{},
		}),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, err
	}
	return &s3Backend{client: client, bucket: u.Host, prefix: strings.Trim(u.Path, "/")}, nil
}

func (b *s3Backend) objectKey(key string) string {
	if b.prefix == "" {
		return key
	}
	return b.prefix + "/" + key
}

func (b *s3Backend) open(create bool) error {
	// A vault always lives in a pre-existing bucket; we never create buckets.
	ok, err := b.client.BucketExists(context.Background(), b.bucket)
	if err != nil {
		return fmt.Errorf("checking s3 bucket %q: %w", b.bucket, err)
	}
	if !ok {
		return fmt.Errorf("s3 bucket %q does not exist", b.bucket)
	}
	return nil
}

func (b *s3Backend) get(key string) ([]byte, error) {
	obj, err := b.client.GetObject(context.Background(), b.bucket, b.objectKey(key), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, fmt.Errorf("%s: %w", key, errNotFound)
		}
		return nil, err
	}
	return data, nil
}

func (b *s3Backend) put(key string, data []byte) error {
	_, err := b.client.PutObject(context.Background(), b.bucket, b.objectKey(key),
		bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
	return err
}

func (b *s3Backend) exists(key string) (bool, error) {
	_, err := b.client.StatObject(context.Background(), b.bucket, b.objectKey(key), minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (b *s3Backend) list(prefix string) ([]objectInfo, error) {
	strip := ""
	if b.prefix != "" {
		strip = b.prefix + "/"
	}
	var out []objectInfo
	for o := range b.client.ListObjects(context.Background(), b.bucket, minio.ListObjectsOptions{
		Prefix:    b.objectKey(prefix),
		Recursive: true,
	}) {
		if o.Err != nil {
			return nil, o.Err
		}
		out = append(out, objectInfo{key: strings.TrimPrefix(o.Key, strip), size: o.Size})
	}
	return out, nil
}
