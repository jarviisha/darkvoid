package service

import (
	"bufio"
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	featurestorage "github.com/jarviisha/darkvoid/internal/feature/storage"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

// MediaType classifies the uploaded file.
type MediaType string

const (
	MediaTypeImage MediaType = "image"
	MediaTypeVideo MediaType = "video"
)

// UploadResult is returned after a successful upload.
type UploadResult struct {
	Key       string    `json:"key"`
	URL       string    `json:"url"`
	MediaType MediaType `json:"media_type"`
}

// allowed MIME types and their corresponding MediaType + extension
var allowedMIME = map[string]struct {
	mediaType MediaType
	ext       string
}{
	"image/jpeg": {MediaTypeImage, ".jpg"},
	"image/png":  {MediaTypeImage, ".png"},
	"image/webp": {MediaTypeImage, ".webp"},
	"image/gif":  {MediaTypeImage, ".gif"},
	"video/mp4":  {MediaTypeVideo, ".mp4"},
	"video/webm": {MediaTypeVideo, ".webm"},
}

const (
	maxImageSize   int64 = 10 << 20  // 10 MB
	maxVideoSize   int64 = 100 << 20 // 100 MB
	mediaSniffSize       = 512
)

// MediaService handles validation and upload of media files.
type MediaService struct {
	storage storage.Storage
}

// NewMediaService creates a new MediaService.
func NewMediaService(s storage.Storage) *MediaService {
	return &MediaService{storage: s}
}

// Upload validates the file's leading bytes and uploads it under the "media/"
// prefix. Client-supplied MIME types and filenames are deliberately ignored.
func (s *MediaService) Upload(ctx context.Context, r io.Reader, size int64) (*UploadResult, error) {
	reader := bufio.NewReaderSize(r, mediaSniffSize)
	header, err := reader.Peek(mediaSniffSize)
	if err != nil && !stderrors.Is(err, io.EOF) {
		return nil, featurestorage.ErrUnsupportedType
	}
	mime := http.DetectContentType(header)

	meta, ok := allowedMIME[mime]
	if !ok {
		logger.Warn(ctx, "unsupported media type", "detected_content_type", mime)
		return nil, featurestorage.ErrUnsupportedType
	}

	maxSize := maxImageSize
	if meta.mediaType == MediaTypeVideo {
		maxSize = maxVideoSize
	}
	if size > maxSize {
		logger.Warn(ctx, "file too large", "size", size, "max", maxSize)
		return nil, featurestorage.ErrFileTooLarge
	}

	key := fmt.Sprintf("media/%s%s", uuid.New().String(), meta.ext)

	if err := s.storage.Put(ctx, key, reader, size, mime); err != nil {
		logger.LogError(ctx, err, "failed to upload media", "key", key)
		return nil, errors.NewInternalError(err)
	}

	logger.Info(ctx, "media uploaded", "key", key, "media_type", meta.mediaType)
	return &UploadResult{
		Key:       key,
		URL:       s.storage.URL(key),
		MediaType: meta.mediaType,
	}, nil
}
