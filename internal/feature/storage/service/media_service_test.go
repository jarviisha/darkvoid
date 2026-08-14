package service

import (
	"bytes"
	"context"
	stderrors "errors"
	"io"
	"strings"
	"testing"

	featurestorage "github.com/jarviisha/darkvoid/internal/feature/storage"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

type recordingStorage struct {
	putCalls    int
	key         string
	contentType string
	body        []byte
	putErr      error
}

func (s *recordingStorage) Put(_ context.Context, key string, r io.Reader, _ int64, contentType string) error {
	s.putCalls++
	s.key = key
	s.contentType = contentType
	s.body, _ = io.ReadAll(r)
	return s.putErr
}

func TestUpload_EnforcesTypeSpecificSizeLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		size int64
	}{
		{name: "image", data: []byte("\x89PNG\r\n\x1a\n"), size: maxImageSize + 1},
		{name: "video", data: []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom<\x06t\xbfmdat"), size: maxVideoSize + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &recordingStorage{}
			_, err := NewMediaService(store).Upload(context.Background(), bytes.NewReader(tt.data), tt.size)
			if !errors.Is(err, featurestorage.ErrFileTooLarge) {
				t.Fatalf("Upload() error = %v, want ErrFileTooLarge", err)
			}
			if store.putCalls != 0 {
				t.Fatalf("Put calls = %d, want 0", store.putCalls)
			}
		})
	}
}

func TestUpload_WrapsStorageFailure(t *testing.T) {
	t.Parallel()
	sentinel := stderrors.New("storage unavailable")
	store := &recordingStorage{putErr: sentinel}
	_, err := NewMediaService(store).Upload(context.Background(), bytes.NewReader([]byte("GIF89a")), 6)
	appErr := errors.GetAppError(err)
	if appErr == nil || !stderrors.Is(appErr, sentinel) {
		t.Fatalf("Upload() error = %v, want internal error wrapping sentinel", err)
	}
}

func (*recordingStorage) Delete(context.Context, string) error { return nil }
func (*recordingStorage) URL(key string) string                { return "https://cdn.test/" + key }

func TestUpload_DetectsContentFromBytes(t *testing.T) {
	store := &recordingStorage{}
	svc := NewMediaService(store)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)

	result, err := svc.Upload(context.Background(), bytes.NewReader(png), int64(len(png)))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if !strings.HasSuffix(result.Key, ".png") {
		t.Fatalf("key = %q, want .png suffix", result.Key)
	}
	if store.contentType != "image/png" {
		t.Fatalf("content type = %q, want image/png", store.contentType)
	}
	if !bytes.Equal(store.body, png) {
		t.Fatal("storage did not receive the complete file")
	}
}

func TestUpload_RejectsHTMLDisguisedAsMedia(t *testing.T) {
	store := &recordingStorage{}
	svc := NewMediaService(store)

	_, err := svc.Upload(context.Background(), strings.NewReader("<html><script>alert(1)</script></html>"), 42)
	if !errors.Is(err, featurestorage.ErrUnsupportedType) {
		t.Fatalf("Upload() error = %v, want ErrUnsupportedType", err)
	}
	if store.putCalls != 0 {
		t.Fatalf("Put calls = %d, want 0", store.putCalls)
	}
}

func TestUpload_AcceptsEveryDocumentedSignature(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		contentType string
		extension   string
	}{
		{name: "jpeg", data: []byte("\xff\xd8\xff\xdb\x00\x84"), contentType: "image/jpeg", extension: ".jpg"},
		{name: "png", data: []byte("\x89PNG\r\n\x1a\n"), contentType: "image/png", extension: ".png"},
		{name: "webp", data: []byte("RIFF\x00\x00\x00\x00WEBPVP"), contentType: "image/webp", extension: ".webp"},
		{name: "gif", data: []byte("GIF89a"), contentType: "image/gif", extension: ".gif"},
		{name: "mp4", data: []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom<\x06t\xbfmdat"), contentType: "video/mp4", extension: ".mp4"},
		{name: "webm", data: []byte("\x1a\x45\xdf\xa3"), contentType: "video/webm", extension: ".webm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingStorage{}
			svc := NewMediaService(store)

			result, err := svc.Upload(context.Background(), bytes.NewReader(tt.data), int64(len(tt.data)))
			if err != nil {
				t.Fatalf("Upload() error = %v", err)
			}
			if store.contentType != tt.contentType {
				t.Fatalf("content type = %q, want %q", store.contentType, tt.contentType)
			}
			if !strings.HasSuffix(result.Key, tt.extension) {
				t.Fatalf("key = %q, want %q suffix", result.Key, tt.extension)
			}
		})
	}
}
