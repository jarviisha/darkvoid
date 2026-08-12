package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

type recordingS3Uploader struct {
	input *transfermanager.UploadObjectInput
	err   error
}

func (u *recordingS3Uploader) UploadObject(_ context.Context, input *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	u.input = input
	return &transfermanager.UploadObjectOutput{}, u.err
}

type recordingS3Client struct {
	deleteInput *awss3.DeleteObjectInput
	headInput   *awss3.HeadBucketInput
	deleteErr   error
	headErr     error
}

func (c *recordingS3Client) DeleteObject(_ context.Context, input *awss3.DeleteObjectInput, _ ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error) {
	c.deleteInput = input
	return &awss3.DeleteObjectOutput{}, c.deleteErr
}

func (c *recordingS3Client) HeadBucket(_ context.Context, input *awss3.HeadBucketInput, _ ...func(*awss3.Options)) (*awss3.HeadBucketOutput, error) {
	c.headInput = input
	return &awss3.HeadBucketOutput{}, c.headErr
}

func TestS3Storage_PutPreservesObjectMetadata(t *testing.T) {
	uploader := &recordingS3Uploader{}
	store := newS3Storage("darkvoid-media", "https://cdn.example.com/assets", uploader, &recordingS3Client{})

	if err := store.Put(context.Background(), "media/post.jpg", strings.NewReader("image"), 5, "image/jpeg"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if uploader.input == nil {
		t.Fatal("uploader was not called")
	}
	if got := *uploader.input.Bucket; got != "darkvoid-media" {
		t.Fatalf("bucket = %q", got)
	}
	if got := *uploader.input.Key; got != "media/post.jpg" {
		t.Fatalf("key = %q", got)
	}
	if got := *uploader.input.ContentLength; got != 5 {
		t.Fatalf("content length = %d", got)
	}
	if got := *uploader.input.ContentType; got != "image/jpeg" {
		t.Fatalf("content type = %q", got)
	}
	data, err := io.ReadAll(uploader.input.Body)
	if err != nil || string(data) != "image" {
		t.Fatalf("body = %q, %v", data, err)
	}
}

func TestS3Storage_DeleteAndHealthCheckUseConfiguredBucket(t *testing.T) {
	client := &recordingS3Client{}
	store := newS3Storage("darkvoid-media", "https://cdn.example.com", &recordingS3Uploader{}, client)

	if err := store.Delete(context.Background(), "avatars/user.jpg"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if client.deleteInput == nil || *client.deleteInput.Bucket != "darkvoid-media" || *client.deleteInput.Key != "avatars/user.jpg" {
		t.Fatalf("delete input = %+v", client.deleteInput)
	}
	if err := store.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if client.headInput == nil || *client.headInput.Bucket != "darkvoid-media" {
		t.Fatalf("head input = %+v", client.headInput)
	}
}

func TestS3Storage_PropagatesProviderFailures(t *testing.T) {
	sentinel := errors.New("object store unavailable")
	store := newS3Storage(
		"darkvoid-media",
		"https://cdn.example.com",
		&recordingS3Uploader{err: sentinel},
		&recordingS3Client{deleteErr: sentinel, headErr: sentinel},
	)

	if err := store.Put(context.Background(), "media/a.jpg", strings.NewReader("x"), 1, "image/jpeg"); !errors.Is(err, sentinel) {
		t.Fatalf("Put error = %v", err)
	}
	if err := store.Delete(context.Background(), "media/a.jpg"); !errors.Is(err, sentinel) {
		t.Fatalf("Delete error = %v", err)
	}
	if err := store.HealthCheck(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("HealthCheck error = %v", err)
	}
}

func TestS3Storage_URLUsesPublicCDNBase(t *testing.T) {
	store := newS3Storage("darkvoid-media", "https://cdn.example.com/assets/", &recordingS3Uploader{}, &recordingS3Client{})
	if got, want := store.URL("avatars/user.jpg"), "https://cdn.example.com/assets/avatars/user.jpg"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
}
