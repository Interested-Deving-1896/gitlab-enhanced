package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Backend stores objects in an S3-compatible bucket.
// Compatible with AWS S3, MinIO, Ceph RGW, Cloudflare R2.
// Activated only when cloud.enabled=true.
type S3Backend struct {
	bucket   string
	region   string
	endpoint string
	client   *s3.Client
	uploader *manager.Uploader
}

// S3Options configures the S3 backend.
type S3Options struct {
	Bucket          string
	Region          string
	Endpoint        string // leave empty for AWS; set for MinIO/Ceph/R2
	AccessKeyID     string // leave empty to use default credential chain
	SecretAccessKey string
}

func NewS3Backend(opts S3Options) (*S3Backend, error) {
	cfgOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(opts.Region),
	}
	if opts.AccessKeyID != "" && opts.SecretAccessKey != "" {
		cfgOpts = append(cfgOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(opts.AccessKeyID, opts.SecretAccessKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), cfgOpts...)
	if err != nil {
		return nil, fmt.Errorf("s3: loading AWS config: %w", err)
	}
	clientOpts := []func(*s3.Options){}
	if opts.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(opts.Endpoint)
			o.UsePathStyle = true // required for MinIO/Ceph
		})
	}
	client := s3.NewFromConfig(awsCfg, clientOpts...)
	return &S3Backend{
		bucket:   opts.Bucket,
		region:   opts.Region,
		endpoint: opts.Endpoint,
		client:   client,
		uploader: manager.NewUploader(client),
	}, nil
}

func (b *S3Backend) Name() string {
	if b.endpoint != "" {
		return fmt.Sprintf("s3://%s@%s", b.bucket, b.endpoint)
	}
	return fmt.Sprintf("s3://%s (%s)", b.bucket, b.region)
}

func (b *S3Backend) Available(ctx context.Context) bool {
	_, err := b.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(b.bucket),
	})
	return err == nil
}

func (b *S3Backend) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := b.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(b.bucket),
		Key:           aws.String(key),
		Body:          r,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("s3 put %q: %w", key, err)
	}
	return nil
}

func (b *S3Backend) Get(ctx context.Context, key string) (io.ReadCloser, *Object, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("s3 get %q: %w", key, err)
	}
	obj := &Object{Key: key, Size: aws.ToInt64(out.ContentLength)}
	if out.LastModified != nil {
		obj.ModTime = *out.LastModified
	}
	if out.ContentType != nil {
		obj.ContentType = *out.ContentType
	}
	return out.Body, obj, nil
}

func (b *S3Backend) Delete(ctx context.Context, key string) error {
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err != nil && !isS3NotFound(err) {
		return fmt.Errorf("s3 delete %q: %w", key, err)
	}
	return nil
}

func (b *S3Backend) Stat(ctx context.Context, key string) (*Object, error) {
	out, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("s3 stat %q: %w", key, err)
	}
	obj := &Object{Key: key, Size: aws.ToInt64(out.ContentLength)}
	if out.LastModified != nil {
		obj.ModTime = *out.LastModified
	}
	if out.ContentType != nil {
		obj.ContentType = *out.ContentType
	}
	return obj, nil
}

func (b *S3Backend) List(ctx context.Context, prefix string) ([]Object, error) {
	var objects []Object
	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list %q: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			o := Object{Key: aws.ToString(obj.Key), Size: aws.ToInt64(obj.Size)}
			if obj.LastModified != nil {
				o.ModTime = *obj.LastModified
			}
			objects = append(objects, o)
		}
	}
	return objects, nil
}

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	switch err.(type) {
	case *types.NoSuchKey, *types.NotFound:
		return true
	}
	return false
}

var _ Backend = (*S3Backend)(nil)
