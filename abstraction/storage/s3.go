package storage

// s3.go — retained for backwards compatibility.
// NewS3Backend is a convenience constructor that creates a CloudBackend
// pre-configured for AWS S3 or an S3-compatible provider.
//
// Prefer NewCloudBackend with explicit CloudOptions for new code.
// The AWS SDK is no longer a direct dependency of this package;
// gocloud.dev/blob/s3blob pulls it in only when this backend is used.

// S3Options configures an S3-compatible backend.
// Kept for API compatibility with existing callers.
type S3Options struct {
	// Bucket name.
	Bucket string

	// Region (e.g. "us-east-1"). Required for AWS; defaults to "us-east-1" for others.
	Region string

	// Endpoint overrides the default S3 URL.
	// Required for MinIO, Ceph, Cloudflare R2. Leave empty for AWS.
	Endpoint string

	// Provider selects the S3-compatible service.
	// Values: aws (default) | minio | ceph | r2
	// Defaults to "aws" when empty.
	Provider string

	// AccessKeyID and SecretAccessKey for explicit credentials.
	// Leave empty to use the default credential chain
	// (AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY env vars, ~/.aws/credentials,
	// EC2 instance metadata, ECS task role, etc.).
	AccessKeyID     string
	SecretAccessKey string
}

// NewS3Backend creates a CloudBackend for an S3 or S3-compatible provider.
func NewS3Backend(opts S3Options) (*CloudBackend, error) {
	provider := opts.Provider
	if provider == "" {
		provider = "aws"
	}
	return NewCloudBackend(CloudOptions{
		Provider:        provider,
		Bucket:          opts.Bucket,
		Region:          opts.Region,
		Endpoint:        opts.Endpoint,
		AccessKeyID:     opts.AccessKeyID,
		SecretAccessKey: opts.SecretAccessKey,
	})
}
