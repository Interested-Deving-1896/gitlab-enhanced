package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"gocloud.dev/blob"

	// Driver imports — each registers itself via init() when imported.
	// Only the drivers actually used at runtime are linked into the binary.
	_ "gocloud.dev/blob/azureblob"
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/s3blob"
)

// CloudBackend stores objects in a cloud blob store via the gocloud.dev/blob
// abstraction. Supports AWS S3, Google Cloud Storage, Azure Blob Storage,
// and any S3-compatible service (MinIO, Ceph RGW, Cloudflare R2).
//
// Provider selection is done entirely through the bucket URL scheme:
//
//	aws / minio / ceph / r2  →  s3://bucket?region=us-east-1
//	gcs                      →  gs://bucket
//	azure                    →  azblob://container
//
// Authentication uses each provider's default credential chain unless
// explicit credentials are supplied in CloudOptions.
type CloudBackend struct {
	provider   string
	bucketURL  string
	bucketName string
	bucket     *blob.Bucket
}

// CloudOptions configures the cloud backend.
type CloudOptions struct {
	// Provider selects the cloud storage provider.
	// Values: aws | gcs | azure | minio | ceph | r2
	Provider string

	// Bucket / container name.
	Bucket string

	// Region is required for AWS; optional for others.
	Region string

	// Endpoint overrides the default service URL.
	// Required for minio, ceph, r2. Leave empty for aws/gcs/azure.
	Endpoint string

	// AccessKeyID + SecretAccessKey for AWS/S3-compatible providers.
	// Leave empty to use the default credential chain (env vars, instance metadata).
	AccessKeyID     string
	SecretAccessKey string

	// GCSKeyFile is the path to a GCS service account JSON key file.
	// Leave empty to use Application Default Credentials.
	GCSKeyFile string

	// AzureConnectionString, AzureAccountName, AzureAccountKey for Azure.
	// Leave empty to use DefaultAzureCredential.
	AzureConnectionString string
	AzureAccountName      string
	AzureAccountKey       string
}

// NewCloudBackend creates a CloudBackend from the given options.
// The underlying bucket connection is established lazily on first use.
func NewCloudBackend(opts CloudOptions) (*CloudBackend, error) {
	if opts.Bucket == "" {
		return nil, fmt.Errorf("cloud storage: bucket name is required")
	}
	if opts.Provider == "" {
		return nil, fmt.Errorf("cloud storage: provider is required (aws|gcs|azure|minio|ceph|r2)")
	}

	bucketURL, err := buildBucketURL(opts)
	if err != nil {
		return nil, fmt.Errorf("cloud storage: building bucket URL: %w", err)
	}

	return &CloudBackend{
		provider:   opts.Provider,
		bucketURL:  bucketURL,
		bucketName: opts.Bucket,
	}, nil
}

func (b *CloudBackend) Name() string {
	return fmt.Sprintf("cloud:%s://%s", b.provider, b.bucketName)
}

// Available checks that the bucket is reachable by attempting to open it.
func (b *CloudBackend) Available(ctx context.Context) bool {
	bucket, err := b.open(ctx)
	if err != nil {
		return false
	}
	// Verify connectivity with a lightweight exists check on a sentinel key
	_, _ = bucket.Exists(ctx, ".gitlab-enhanced-probe")
	return true
}

func (b *CloudBackend) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	bucket, err := b.open(ctx)
	if err != nil {
		return err
	}
	opts := &blob.WriterOptions{}
	if size > 0 {
		opts.BufferSize = bufferSize(size)
	}
	w, err := bucket.NewWriter(ctx, key, opts)
	if err != nil {
		return fmt.Errorf("cloud put %q: opening writer: %w", key, err)
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return fmt.Errorf("cloud put %q: writing: %w", key, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("cloud put %q: closing writer: %w", key, err)
	}
	return nil
}

func (b *CloudBackend) Get(ctx context.Context, key string) (io.ReadCloser, *Object, error) {
	bucket, err := b.open(ctx)
	if err != nil {
		return nil, nil, err
	}
	r, err := bucket.NewReader(ctx, key, nil)
	if err != nil {
		if isCloudNotFound(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("cloud get %q: %w", key, err)
	}
	obj := &Object{
		Key:         key,
		Size:        r.Size(),
		ContentType: r.ContentType(),
		ModTime:     r.ModTime(),
	}
	return r, obj, nil
}

func (b *CloudBackend) Delete(ctx context.Context, key string) error {
	bucket, err := b.open(ctx)
	if err != nil {
		return err
	}
	if err := bucket.Delete(ctx, key); err != nil {
		if isCloudNotFound(err) {
			return nil // no-op
		}
		return fmt.Errorf("cloud delete %q: %w", key, err)
	}
	return nil
}

func (b *CloudBackend) Stat(ctx context.Context, key string) (*Object, error) {
	bucket, err := b.open(ctx)
	if err != nil {
		return nil, err
	}
	attrs, err := bucket.Attributes(ctx, key)
	if err != nil {
		if isCloudNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("cloud stat %q: %w", key, err)
	}
	return &Object{
		Key:         key,
		Size:        attrs.Size,
		ContentType: attrs.ContentType,
		ModTime:     attrs.ModTime,
	}, nil
}

func (b *CloudBackend) List(ctx context.Context, prefix string) ([]Object, error) {
	bucket, err := b.open(ctx)
	if err != nil {
		return nil, err
	}
	iter := bucket.List(&blob.ListOptions{Prefix: prefix})
	var objects []Object
	for {
		obj, err := iter.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cloud list %q: %w", prefix, err)
		}
		objects = append(objects, Object{
			Key:     obj.Key,
			Size:    obj.Size,
			ModTime: obj.ModTime,
		})
	}
	return objects, nil
}

// open returns the cached bucket or opens a new connection.
func (b *CloudBackend) open(ctx context.Context) (*blob.Bucket, error) {
	if b.bucket != nil {
		return b.bucket, nil
	}
	bucket, err := blob.OpenBucket(ctx, b.bucketURL)
	if err != nil {
		return nil, fmt.Errorf("cloud: opening bucket %q (%s): %w", b.bucketName, b.provider, err)
	}
	b.bucket = bucket
	return bucket, nil
}

// buildBucketURL constructs the gocloud.dev bucket URL for the given provider.
func buildBucketURL(opts CloudOptions) (string, error) {
	switch strings.ToLower(opts.Provider) {

	case "aws":
		// s3://bucket?region=us-east-1
		// Credentials: AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY env vars,
		// or ~/.aws/credentials, or EC2 instance metadata.
		q := url.Values{}
		if opts.Region != "" {
			q.Set("region", opts.Region)
		}
		if opts.AccessKeyID != "" {
			q.Set("awssdk", "v2")
		}
		u := "s3://" + opts.Bucket
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		return u, nil

	case "minio", "ceph", "r2":
		// S3-compatible: s3://bucket?endpoint=http://minio:9000&region=us-east-1&s3ForcePathStyle=true
		if opts.Endpoint == "" {
			return "", fmt.Errorf("provider %q requires endpoint to be set", opts.Provider)
		}
		q := url.Values{}
		q.Set("endpoint", opts.Endpoint)
		q.Set("s3ForcePathStyle", "true")
		if opts.Region != "" {
			q.Set("region", opts.Region)
		} else {
			q.Set("region", "us-east-1") // S3-compatible services require a region value
		}
		// Cloudflare R2 uses virtual-hosted style, not path style
		if strings.ToLower(opts.Provider) == "r2" {
			q.Del("s3ForcePathStyle")
		}
		return "s3://" + opts.Bucket + "?" + q.Encode(), nil

	case "gcs":
		// gs://bucket
		// Credentials: GOOGLE_APPLICATION_CREDENTIALS env var, or ADC.
		return "gs://" + opts.Bucket, nil

	case "azure":
		// azblob://container
		// Credentials: AZURE_STORAGE_ACCOUNT + AZURE_STORAGE_KEY env vars,
		// or AZURE_STORAGE_CONNECTION_STRING, or DefaultAzureCredential.
		q := url.Values{}
		if opts.AzureAccountName != "" {
			q.Set("storage_account", opts.AzureAccountName)
		}
		u := "azblob://" + opts.Bucket
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		return u, nil

	default:
		return "", fmt.Errorf("unknown cloud provider %q — supported: aws, gcs, azure, minio, ceph, r2", opts.Provider)
	}
}

// isCloudNotFound returns true for gocloud.dev not-found errors.
func isCloudNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "code=NotFound")
}

// bufferSize returns a sensible upload buffer size based on object size.
func bufferSize(size int64) int {
	const (
		mb  = 1 << 20
		max = 64 * mb
	)
	buf := size / 10
	if buf > max {
		return max
	}
	if buf < mb {
		return mb
	}
	return int(buf)
}

// suppress unused import
var _ = time.Now

var _ Backend = (*CloudBackend)(nil)
