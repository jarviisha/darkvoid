package storage

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

func TestS3Storage_RealBucketLifecycle(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_TEST_ENDPOINT not set")
	}
	accessKey := os.Getenv("S3_TEST_ACCESS_KEY_ID")
	secretKey := os.Getenv("S3_TEST_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		t.Fatal("S3_TEST_ACCESS_KEY_ID and S3_TEST_SECRET_ACCESS_KEY are required with S3_TEST_ENDPOINT")
	}

	ctx := context.Background()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		t.Fatalf("load integration AWS config: %v", err)
	}
	client := awss3.NewFromConfig(awsCfg, func(options *awss3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})
	bucket := "darkvoid-test-" + uuid.NewString()
	if _, createErr := client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String(bucket)}); createErr != nil {
		t.Fatalf("create integration bucket: %v", createErr)
	}
	t.Cleanup(func() {
		_, _ = client.DeleteBucket(context.Background(), &awss3.DeleteBucketInput{Bucket: aws.String(bucket)})
	})

	store, err := NewS3(ctx, S3Config{
		Endpoint: endpoint, Region: "us-east-1", Bucket: bucket,
		AccessKeyID: accessKey, SecretAccessKey: secretKey, UsePathStyle: true,
	}, "https://cdn.test/media")
	if err != nil {
		t.Fatalf("NewS3() error = %v", err)
	}

	const key = "media/integration.txt"
	const body = "shared object storage integration"
	if putErr := store.Put(ctx, key, strings.NewReader(body), int64(len(body)), "text/plain"); putErr != nil {
		t.Fatalf("Put() error = %v", putErr)
	}
	object, err := client.GetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	gotBody, readErr := io.ReadAll(object.Body)
	closeErr := object.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read/close object = %v/%v", readErr, closeErr)
	}
	if string(gotBody) != body {
		t.Fatalf("object body = %q, want %q", gotBody, body)
	}
	if object.ContentType == nil || *object.ContentType != "text/plain" {
		t.Fatalf("content type = %v, want text/plain", object.ContentType)
	}
	if object.CacheControl == nil || *object.CacheControl != "public, max-age=31536000, immutable" {
		t.Fatalf("cache control = %v", object.CacheControl)
	}
	if checker, ok := store.(HealthChecker); !ok {
		t.Fatal("S3 store does not implement HealthChecker")
	} else if err := checker.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if got := store.URL(key); got != "https://cdn.test/media/"+key {
		t.Fatalf("URL() = %q", got)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := client.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}); err == nil {
		t.Fatal("HeadObject() succeeded after Delete()")
	}
}
