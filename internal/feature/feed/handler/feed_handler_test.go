package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
	httputil "github.com/jarviisha/darkvoid/internal/http"
	pkgerrors "github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

type mockFeedService struct {
	getFeed     func(ctx context.Context, userID uuid.UUID, cursor *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error)
	getDiscover func(ctx context.Context, viewerID *uuid.UUID, cursor *feed.DiscoverCursor, limit int32) ([]*feedentity.Post, *feed.DiscoverCursor, error)
}

func (m *mockFeedService) GetFeed(ctx context.Context, userID uuid.UUID, cursor *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
	if m.getFeed != nil {
		return m.getFeed(ctx, userID, cursor)
	}
	return nil, nil, nil
}

func (m *mockFeedService) GetDiscover(ctx context.Context, viewerID *uuid.UUID, cursor *feed.DiscoverCursor, limit int32) ([]*feedentity.Post, *feed.DiscoverCursor, error) {
	if m.getDiscover != nil {
		return m.getDiscover(ctx, viewerID, cursor, limit)
	}
	return nil, nil, nil
}

func samplePost(authorID uuid.UUID) *feedentity.Post {
	return &feedentity.Post{
		ID:         uuid.New(),
		AuthorID:   authorID,
		Content:    "Hello world",
		Visibility: "public",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func sampleFeedItem(authorID uuid.UUID) *feedentity.FeedItem {
	return &feedentity.FeedItem{
		Post:   samplePost(authorID),
		Score:  42.0,
		Source: feedentity.SourceFollowing,
	}
}

func nopStore() storage.Storage { return storage.NewNop("") }

func newFeedHandler(svc feedService) *FeedHandler {
	return &FeedHandler{feedService: svc, store: nopStore()}
}

func withAuth(r *http.Request, userID uuid.UUID) *http.Request {
	return r.WithContext(httputil.WithUserID(r.Context(), userID))
}

func assertStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Errorf("expected status %d, got %d (body: %s)", want, w.Code, w.Body.String())
	}
}

// GetFeed tests

func TestGetFeed_Success(t *testing.T) {
	userID := uuid.New()
	svc := &mockFeedService{
		getFeed: func(_ context.Context, _ uuid.UUID, _ *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
			return []*feedentity.FeedItem{sampleFeedItem(uuid.New())}, nil, nil
		},
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/feed", nil)
	r := withAuth(req, userID)
	w := httptest.NewRecorder()
	newFeedHandler(svc).GetFeed(w, r)
	assertStatus(t, w, http.StatusOK)
}

func TestGetFeed_WithNextCursor(t *testing.T) {
	userID := uuid.New()
	next := &feed.FeedCursor{
		Version:  feed.FeedCursorVersion,
		Timeline: &feed.TimelinePosition{Score: time.Now().UnixMicro(), PostID: uuid.New().String()},
		IssuedAt: time.Now().UTC(),
	}
	svc := &mockFeedService{
		getFeed: func(_ context.Context, _ uuid.UUID, _ *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
			return []*feedentity.FeedItem{sampleFeedItem(uuid.New())}, next, nil
		},
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/feed", nil)
	r := withAuth(req, userID)
	w := httptest.NewRecorder()
	newFeedHandler(svc).GetFeed(w, r)
	assertStatus(t, w, http.StatusOK)

	var body FeedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.NextCursor == "" {
		t.Fatal("expected next_cursor")
	}
	if _, err := feed.DecodeFeedCursor(body.NextCursor); err != nil {
		t.Fatalf("next cursor is not v2 decodable: %v", err)
	}
}

func TestGetFeed_WithRecommendationMetadata(t *testing.T) {
	userID := uuid.New()
	score := 0.91
	rank := 1
	svc := &mockFeedService{
		getFeed: func(_ context.Context, _ uuid.UUID, _ *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
			item := sampleFeedItem(uuid.New())
			item.Source = feedentity.SourceRecommendation
			item.RecommendationScore = &score
			item.RecommendationRank = &rank
			return []*feedentity.FeedItem{item}, nil, nil
		},
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/feed", nil)
	r := withAuth(req, userID)
	w := httptest.NewRecorder()
	newFeedHandler(svc).GetFeed(w, r)
	assertStatus(t, w, http.StatusOK)

	var body FeedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Data[0].RecommendationScore == nil || *body.Data[0].RecommendationScore != score {
		t.Fatalf("recommendation score not returned: %+v", body.Data[0])
	}
	if body.Data[0].RecommendationRank == nil || *body.Data[0].RecommendationRank != rank {
		t.Fatalf("recommendation rank not returned: %+v", body.Data[0])
	}
}

func TestGetFeed_InvalidCursor(t *testing.T) {
	userID := uuid.New()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/feed?cursor=not-valid!!!", nil)
	r := withAuth(req, userID)
	w := httptest.NewRecorder()
	newFeedHandler(&mockFeedService{}).GetFeed(w, r)
	assertStatus(t, w, http.StatusBadRequest)

	var body pkgerrors.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if body.Error.Code != "BAD_REQUEST" || body.Error.Message != "invalid cursor" {
		t.Fatalf("unexpected error response: %+v", body)
	}
}

func TestGetFeed_UnsupportedCursorVersion(t *testing.T) {
	userID := uuid.New()
	cursor := (&feed.FeedCursor{Version: feed.FeedCursorVersion + 1}).Encode()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/feed?cursor="+cursor, nil)
	r := withAuth(req, userID)
	w := httptest.NewRecorder()
	newFeedHandler(&mockFeedService{}).GetFeed(w, r)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestGetFeed_RejectsLegacySessionCursor(t *testing.T) {
	userID := uuid.New()
	raw, err := json.Marshal(map[string]any{
		"version":       1,
		"session_id":    uuid.NewString(),
		"mode":          "mixed",
		"pending_items": []map[string]string{{"post_id": uuid.NewString()}},
		"seen_post_ids": []string{uuid.NewString()},
		"created_at":    time.Now().UTC(),
		"expires_at":    time.Now().UTC().Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("marshal legacy cursor: %v", err)
	}
	cursor := base64.RawURLEncoding.EncodeToString(raw)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/feed?cursor="+cursor, nil)
	r := withAuth(req, userID)
	w := httptest.NewRecorder()
	newFeedHandler(&mockFeedService{}).GetFeed(w, r)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestGetFeed_Unauthenticated(t *testing.T) {
	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/feed", nil)
	w := httptest.NewRecorder()
	newFeedHandler(&mockFeedService{}).GetFeed(w, r)
	assertStatus(t, w, http.StatusUnauthorized)
}

func TestGetFeed_ServiceError(t *testing.T) {
	userID := uuid.New()
	svc := &mockFeedService{
		getFeed: func(_ context.Context, _ uuid.UUID, _ *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
			return nil, nil, pkgerrors.NewInternalError(errors.New("db error"))
		},
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/feed", nil)
	r := withAuth(req, userID)
	w := httptest.NewRecorder()
	newFeedHandler(svc).GetFeed(w, r)
	assertStatus(t, w, http.StatusInternalServerError)
}

// GetDiscover tests

func TestGetDiscover_Success(t *testing.T) {
	svc := &mockFeedService{
		getDiscover: func(_ context.Context, _ *uuid.UUID, _ *feed.DiscoverCursor, _ int32) ([]*feedentity.Post, *feed.DiscoverCursor, error) {
			return []*feedentity.Post{samplePost(uuid.New())}, nil, nil
		},
	}
	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/discover", nil)
	w := httptest.NewRecorder()
	newFeedHandler(svc).GetDiscover(w, r)
	assertStatus(t, w, http.StatusOK)
}

func TestGetDiscover_WithNextCursor(t *testing.T) {
	next := &feed.DiscoverCursor{CreatedAt: time.Now(), PostID: uuid.New().String()}
	svc := &mockFeedService{
		getDiscover: func(_ context.Context, _ *uuid.UUID, _ *feed.DiscoverCursor, _ int32) ([]*feedentity.Post, *feed.DiscoverCursor, error) {
			return []*feedentity.Post{samplePost(uuid.New())}, next, nil
		},
	}
	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/discover", nil)
	w := httptest.NewRecorder()
	newFeedHandler(svc).GetDiscover(w, r)
	assertStatus(t, w, http.StatusOK)
}

func TestGetDiscover_PassesCursorUnchanged(t *testing.T) {
	createdAt := time.Now().UTC().Add(-time.Hour)
	postID := uuid.New().String()
	cursor := (&feed.DiscoverCursor{CreatedAt: createdAt, PostID: postID}).Encode()
	svc := &mockFeedService{
		getDiscover: func(_ context.Context, _ *uuid.UUID, got *feed.DiscoverCursor, _ int32) ([]*feedentity.Post, *feed.DiscoverCursor, error) {
			if got == nil || !got.CreatedAt.Equal(createdAt) || got.PostID != postID {
				t.Fatalf("cursor = %+v, want %s/%s", got, createdAt, postID)
			}
			return []*feedentity.Post{samplePost(uuid.New())}, nil, nil
		},
	}
	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/discover?cursor="+cursor, nil)
	w := httptest.NewRecorder()
	newFeedHandler(svc).GetDiscover(w, r)
	assertStatus(t, w, http.StatusOK)
}

func TestGetDiscover_InvalidCursor(t *testing.T) {
	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/discover?cursor=not-valid!!!", nil)
	w := httptest.NewRecorder()
	newFeedHandler(&mockFeedService{}).GetDiscover(w, r)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestGetDiscover_PassesBoundedLimit(t *testing.T) {
	var gotLimit int32
	svc := &mockFeedService{
		getDiscover: func(_ context.Context, _ *uuid.UUID, _ *feed.DiscoverCursor, limit int32) ([]*feedentity.Post, *feed.DiscoverCursor, error) {
			gotLimit = limit
			return nil, nil, nil
		},
	}
	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/discover?limit=120", nil)
	w := httptest.NewRecorder()
	newFeedHandler(svc).GetDiscover(w, r)
	assertStatus(t, w, http.StatusOK)
	if gotLimit != 100 {
		t.Fatalf("limit = %d, want 100", gotLimit)
	}
}

func TestGetDiscover_NoAuthStillWorks(t *testing.T) {
	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/discover", nil)
	w := httptest.NewRecorder()
	newFeedHandler(&mockFeedService{}).GetDiscover(w, r)
	assertStatus(t, w, http.StatusOK)
}

func TestGetDiscover_ServiceError(t *testing.T) {
	svc := &mockFeedService{
		getDiscover: func(_ context.Context, _ *uuid.UUID, _ *feed.DiscoverCursor, _ int32) ([]*feedentity.Post, *feed.DiscoverCursor, error) {
			return nil, nil, pkgerrors.NewInternalError(errors.New("db error"))
		},
	}
	r, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/discover", nil)
	w := httptest.NewRecorder()
	newFeedHandler(svc).GetDiscover(w, r)
	assertStatus(t, w, http.StatusInternalServerError)
}
