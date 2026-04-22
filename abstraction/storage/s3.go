package storage

import (
	"context"
	"fmt"
	"io"
)

// S3Backend stores objects in an S3-compatible bucket.
// This is a stub — full implementation requires cloud.enabled=true.
// Compatible with AWS S3, MinIO, Ceph RGW, Cloudflare R2.
type S3Backend struct {
	bucket string
	region string
}

func NewS3Backend(bucket, region string) *S3Backend {
	return &S3Backend{bucket: bucket, region: region}
}

func (b *S3Backend) Name() string { return fmt.Sprintf("s3://%s (%s)", b.bucket, b.region) }

func (b *S3Backend) Available(_ context.Context) bool {
	// TODO: HEAD bucket check using AWS SDK
	return b.bucket != "" && b.region != ""
}

func (b *S3Backend) Put(_ context.Context, _ string, _ io.Reader, _ int64) error {
	return errNotImplemented("S3Backend.Put")
}

func (b *S3Backend) Get(_ context.Context, _ string) (io.ReadCloser, *Object, error) {
	return nil, nil, errNotImplemented("S3Backend.Get")
}

func (b *S3Backend) Delete(_ context.Context, _ string) error {
	return errNotImplemented("S3Backend.Delete")
}

func (b *S3Backend) Stat(_ context.Context, _ string) (*Object, error) {
	return nil, errNotImplemented("S3Backend.Stat")
}

func (b *S3Backend) List(_ context.Context, _ string) ([]Object, error) {
	return nil, errNotImplemented("S3Backend.List")
}

var _ Backend = (*S3Backend)(nil)
