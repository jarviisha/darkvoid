package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	post "github.com/jarviisha/darkvoid/internal/feature/post"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
)

// --------------------------------------------------------------------------
// Mock: hashtagSvc
// --------------------------------------------------------------------------

type mockHashtagSvc struct {
	getTrending       func(ctx context.Context) ([]*entity.TrendingHashtag, error)
	searchHashtags    func(ctx context.Context, prefix string) ([]string, error)
	getPostsByHashtag func(ctx context.Context, name string, viewerID *uuid.UUID, cursor *post.UserPostCursor, limit int32) ([]*entity.Post, *post.UserPostCursor, error)
}

func (m *mockHashtagSvc) GetTrending(ctx context.Context) ([]*entity.TrendingHashtag, error) {
	if m.getTrending != nil {
		return m.getTrending(ctx)
	}
	return nil, nil
}

func (m *mockHashtagSvc) SearchHashtags(ctx context.Context, prefix string) ([]string, error) {
	if m.searchHashtags != nil {
		return m.searchHashtags(ctx, prefix)
	}
	return nil, nil
}

func (m *mockHashtagSvc) GetPostsByHashtag(ctx context.Context, name string, viewerID *uuid.UUID, cursor *post.UserPostCursor, limit int32) ([]*entity.Post, *post.UserPostCursor, error) {
	if m.getPostsByHashtag != nil {
		return m.getPostsByHashtag(ctx, name, viewerID, cursor, limit)
	}
	return nil, nil, nil
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

func newHashtagHandlerWithSvc(svc hashtagSvc) *HashtagHandler {
	return &HashtagHandler{hashtagService: svc, store: nopStore()}
}

// --------------------------------------------------------------------------
// GetTrendingHashtags tests
// --------------------------------------------------------------------------

func TestGetTrendingHashtags_Success(t *testing.T) {
	svc := &mockHashtagSvc{
		getTrending: func(_ context.Context) ([]*entity.TrendingHashtag, error) {
			return []*entity.TrendingHashtag{{Name: "golang", Count: 42}}, nil
		},
	}
	h := newHashtagHandlerWithSvc(svc)

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/hashtags/trending", nil)
	w := httptest.NewRecorder()

	h.GetTrendingHashtags(w, r)
	assertStatus(t, w, http.StatusOK)

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func TestGetTrendingHashtags_ServiceError(t *testing.T) {
	svc := &mockHashtagSvc{
		getTrending: func(_ context.Context) ([]*entity.TrendingHashtag, error) {
			return nil, fmt.Errorf("unexpected db failure")
		},
	}
	h := newHashtagHandlerWithSvc(svc)

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/hashtags/trending", nil)
	w := httptest.NewRecorder()

	h.GetTrendingHashtags(w, r)
	assertStatus(t, w, http.StatusInternalServerError)
}

// --------------------------------------------------------------------------
// SearchHashtags tests
// --------------------------------------------------------------------------

func TestSearchHashtags_Success(t *testing.T) {
	svc := &mockHashtagSvc{
		searchHashtags: func(_ context.Context, _ string) ([]string, error) {
			return []string{"golang", "gopher"}, nil
		},
	}
	h := newHashtagHandlerWithSvc(svc)

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/hashtags/search?q=go", nil)
	w := httptest.NewRecorder()

	h.SearchHashtags(w, r)
	assertStatus(t, w, http.StatusOK)

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if got, _ := body["prefix"].(string); got != "go" {
		t.Errorf("expected prefix=go, got %q", got)
	}
}

func TestSearchHashtags_QueryTooShort(t *testing.T) {
	h := newHashtagHandlerWithSvc(&mockHashtagSvc{})

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/hashtags/search?q=a", nil)
	w := httptest.NewRecorder()

	h.SearchHashtags(w, r)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestSearchHashtags_ServiceError(t *testing.T) {
	svc := &mockHashtagSvc{
		searchHashtags: func(_ context.Context, _ string) ([]string, error) {
			return nil, fmt.Errorf("unexpected db failure")
		},
	}
	h := newHashtagHandlerWithSvc(svc)

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/hashtags/search?q=go", nil)
	w := httptest.NewRecorder()

	h.SearchHashtags(w, r)
	assertStatus(t, w, http.StatusInternalServerError)
}

// --------------------------------------------------------------------------
// GetPostsByHashtag tests
// --------------------------------------------------------------------------

func TestGetPostsByHashtag_Success(t *testing.T) {
	svc := &mockHashtagSvc{
		getPostsByHashtag: func(_ context.Context, name string, _ *uuid.UUID, _ *post.UserPostCursor, _ int32) ([]*entity.Post, *post.UserPostCursor, error) {
			return []*entity.Post{{ID: uuid.New()}}, nil, nil
		},
	}
	h := newHashtagHandlerWithSvc(svc)

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/hashtags/golang/posts", nil)
	r = withChiParam(r, "name", "golang")
	w := httptest.NewRecorder()

	h.GetPostsByHashtag(w, r)
	assertStatus(t, w, http.StatusOK)
}

func TestGetPostsByHashtag_MissingName(t *testing.T) {
	h := newHashtagHandlerWithSvc(&mockHashtagSvc{})

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/hashtags//posts", nil)
	r = withChiParam(r, "name", "")
	w := httptest.NewRecorder()

	h.GetPostsByHashtag(w, r)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestGetPostsByHashtag_InvalidCursor(t *testing.T) {
	h := newHashtagHandlerWithSvc(&mockHashtagSvc{})

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/hashtags/golang/posts?cursor=!!bad", nil)
	r = withChiParam(r, "name", "golang")
	w := httptest.NewRecorder()

	h.GetPostsByHashtag(w, r)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestGetPostsByHashtag_ServiceError(t *testing.T) {
	svc := &mockHashtagSvc{
		getPostsByHashtag: func(_ context.Context, _ string, _ *uuid.UUID, _ *post.UserPostCursor, _ int32) ([]*entity.Post, *post.UserPostCursor, error) {
			return nil, nil, fmt.Errorf("unexpected db failure")
		},
	}
	h := newHashtagHandlerWithSvc(svc)

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/hashtags/golang/posts", nil)
	r = withChiParam(r, "name", "golang")
	w := httptest.NewRecorder()

	h.GetPostsByHashtag(w, r)
	assertStatus(t, w, http.StatusInternalServerError)
}

func TestGetPostsByHashtag_LimitCappedAt100(t *testing.T) {
	var capturedLimit int32
	svc := &mockHashtagSvc{
		getPostsByHashtag: func(_ context.Context, _ string, _ *uuid.UUID, _ *post.UserPostCursor, limit int32) ([]*entity.Post, *post.UserPostCursor, error) {
			capturedLimit = limit
			return nil, nil, nil
		},
	}
	h := newHashtagHandlerWithSvc(svc)

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/hashtags/golang/posts?limit=200", nil)
	r = withChiParam(r, "name", "golang")
	w := httptest.NewRecorder()

	h.GetPostsByHashtag(w, r)
	assertStatus(t, w, http.StatusOK)
	if capturedLimit != 100 {
		t.Errorf("expected limit capped at 100, got %d", capturedLimit)
	}
}
