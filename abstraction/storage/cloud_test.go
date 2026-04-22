package storage_test

import (
	"strings"
	"testing"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/storage"
)

// TestBuildBucketURL exercises URL construction for every supported provider.
// We test via NewCloudBackend (which calls buildBucketURL internally) and
// inspect the Name() output, which encodes provider and bucket.
func TestNewCloudBackend_ProviderNames(t *testing.T) {
	cases := []struct {
		name     string
		opts     storage.CloudOptions
		wantName string // substring expected in Name()
		wantErr  bool
	}{
		{
			name:     "aws",
			opts:     storage.CloudOptions{Provider: "aws", Bucket: "my-bucket", Region: "us-east-1"},
			wantName: "cloud:aws://my-bucket",
		},
		{
			name:     "gcs",
			opts:     storage.CloudOptions{Provider: "gcs", Bucket: "my-bucket"},
			wantName: "cloud:gcs://my-bucket",
		},
		{
			name:     "azure",
			opts:     storage.CloudOptions{Provider: "azure", Bucket: "my-container"},
			wantName: "cloud:azure://my-container",
		},
		{
			name:     "minio",
			opts:     storage.CloudOptions{Provider: "minio", Bucket: "my-bucket", Endpoint: "http://minio:9000"},
			wantName: "cloud:minio://my-bucket",
		},
		{
			name:     "ceph",
			opts:     storage.CloudOptions{Provider: "ceph", Bucket: "my-bucket", Endpoint: "http://ceph-rgw:7480"},
			wantName: "cloud:ceph://my-bucket",
		},
		{
			name:     "r2",
			opts:     storage.CloudOptions{Provider: "r2", Bucket: "my-bucket", Endpoint: "https://account.r2.cloudflarestorage.com"},
			wantName: "cloud:r2://my-bucket",
		},
		{
			name:    "missing bucket",
			opts:    storage.CloudOptions{Provider: "aws"},
			wantErr: true,
		},
		{
			name:    "missing provider",
			opts:    storage.CloudOptions{Bucket: "my-bucket"},
			wantErr: true,
		},
		{
			name:    "unknown provider",
			opts:    storage.CloudOptions{Provider: "dropbox", Bucket: "my-bucket"},
			wantErr: true,
		},
		{
			name:    "minio without endpoint",
			opts:    storage.CloudOptions{Provider: "minio", Bucket: "my-bucket"},
			wantErr: true,
		},
		{
			name:    "ceph without endpoint",
			opts:    storage.CloudOptions{Provider: "ceph", Bucket: "my-bucket"},
			wantErr: true,
		},
		{
			name:    "r2 without endpoint",
			opts:    storage.CloudOptions{Provider: "r2", Bucket: "my-bucket"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := storage.NewCloudBackend(tc.opts)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (Name: %s)", b.Name())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(b.Name(), tc.wantName) {
				t.Errorf("Name() = %q, want it to contain %q", b.Name(), tc.wantName)
			}
		})
	}
}

// TestNewS3Backend_CompatShim verifies the S3Options shim delegates correctly.
func TestNewS3Backend_CompatShim(t *testing.T) {
	cases := []struct {
		name     string
		opts     storage.S3Options
		wantName string
		wantErr  bool
	}{
		{
			name:     "aws default",
			opts:     storage.S3Options{Bucket: "my-bucket", Region: "eu-west-1"},
			wantName: "cloud:aws://my-bucket",
		},
		{
			name:     "explicit aws provider",
			opts:     storage.S3Options{Provider: "aws", Bucket: "my-bucket"},
			wantName: "cloud:aws://my-bucket",
		},
		{
			name:     "minio via S3Options",
			opts:     storage.S3Options{Provider: "minio", Bucket: "my-bucket", Endpoint: "http://minio:9000"},
			wantName: "cloud:minio://my-bucket",
		},
		{
			name:    "missing bucket",
			opts:    storage.S3Options{Region: "us-east-1"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := storage.NewS3Backend(tc.opts)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(b.Name(), tc.wantName) {
				t.Errorf("Name() = %q, want it to contain %q", b.Name(), tc.wantName)
			}
		})
	}
}
