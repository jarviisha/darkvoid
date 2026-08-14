package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jarviisha/darkvoid/internal/feature/storage/service"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

type handlerStorageStub struct {
	putCalls int
}

func (s *handlerStorageStub) Put(context.Context, string, io.Reader, int64, string) error {
	s.putCalls++
	return nil
}
func (*handlerStorageStub) Delete(context.Context, string) error { return nil }
func (*handlerStorageStub) URL(key string) string                { return "https://cdn.test/" + key }

func multipartRequest(t *testing.T, field string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, "upload.bin")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/media/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestUpload_ReturnsStoredMedia(t *testing.T) {
	t.Parallel()
	store := &handlerStorageStub{}
	handler := NewMediaHandler(service.NewMediaService(store))
	recorder := httptest.NewRecorder()

	handler.Upload(recorder, multipartRequest(t, "file", []byte("GIF89a")))

	if recorder.Code != http.StatusOK || store.putCalls != 1 {
		t.Fatalf("status = %d, put calls = %d", recorder.Code, store.putCalls)
	}
	var response UploadResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.MediaType != "image" || response.Key == "" || response.URL != "https://cdn.test/"+response.Key {
		t.Fatalf("response = %#v", response)
	}
}

func TestUpload_RejectsMalformedOrMissingFile(t *testing.T) {
	t.Parallel()
	store := &handlerStorageStub{}
	handler := NewMediaHandler(service.NewMediaService(store))
	tests := []struct {
		name    string
		request *http.Request
	}{
		{name: "not multipart", request: httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/media/upload", bytes.NewBufferString("invalid"))},
		{name: "missing file field", request: multipartRequest(t, "other", []byte("GIF89a"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.Upload(recorder, tt.request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", recorder.Code)
			}
		})
	}
	if store.putCalls != 0 {
		t.Fatalf("Put calls = %d, want 0", store.putCalls)
	}
}

func TestUpload_PreservesMediaValidationStatus(t *testing.T) {
	t.Parallel()
	handler := NewMediaHandler(service.NewMediaService(storage.NewNop("https://cdn.test")))
	recorder := httptest.NewRecorder()
	handler.Upload(recorder, multipartRequest(t, "file", []byte("<script>alert(1)</script>")))
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", recorder.Code)
	}
}
