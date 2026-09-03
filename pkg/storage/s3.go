package storage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3Uploader interface {
	UploadObject(context.Context, *transfermanager.UploadObjectInput, ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error)
}

type s3ObjectClient interface {
	DeleteObject(context.Context, *awss3.DeleteObjectInput, ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error)
	HeadBucket(context.Context, *awss3.HeadBucketInput, ...func(*awss3.Options)) (*awss3.HeadBucketOutput, error)
}

// s3Storage stores immutable user media in one shared S3-compatible bucket.
type s3Storage struct {
	bucket   string
	baseURL  string
	uploader s3Uploader
	client   s3ObjectClient
}

// NewS3 creates shared object storage backed by AWS S3 or an S3-compatible
// endpoint. Empty static credentials intentionally select the AWS default
// credential chain, allowing IAM roles and workload identity in production.
func NewS3(ctx context.Context, cfg S3Config, baseURL string) (Storage, error) {
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.Region)}
	if cfg.AccessKeyID != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("storage/s3: load AWS configuration: %w", err)
	}
	client := awss3.NewFromConfig(awsCfg, func(options *awss3.Options) {
		options.UsePathStyle = cfg.UsePathStyle
		if cfg.Endpoint != "" {
			options.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})
	return newS3Storage(cfg.Bucket, baseURL, transfermanager.New(client), client), nil
}

func newS3Storage(bucket, baseURL string, uploader s3Uploader, client s3ObjectClient) *s3Storage {
	return &s3Storage{
		bucket:   bucket,
		baseURL:  strings.TrimRight(baseURL, "/"),
		uploader: uploader,
		client:   client,
	}
}

func (s *s3Storage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          r,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
		CacheControl:  aws.String("public, max-age=31536000, immutable"),
	})
	if err != nil {
		return fmt.Errorf("storage/s3: put key %q in bucket %q: %w", key, s.bucket, err)
	}
	return nil
}

func (s *s3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("storage/s3: delete key %q from bucket %q: %w", key, s.bucket, err)
	}
	return nil
}

func (s *s3Storage) URL(key string) string {
	return fmt.Sprintf("%s/%s", s.baseURL, key)
}

func (s *s3Storage) HealthCheck(ctx context.Context) error {
	if _, err := s.client.HeadBucket(ctx, &awss3.HeadBucketInput{Bucket: aws.String(s.bucket)}); err != nil {
		return fmt.Errorf("storage/s3: probe bucket %q: %w", s.bucket, err)
	}
	return nil
}
