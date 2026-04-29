package handler

import (
	"context"
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
	next := &feed.FeedCursor{State: feed.NewFeedPageState(time.Now())}
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
}

func TestGetFeed_ExpiredCursor(t *testing.T) {
	userID := uuid.New()
	state := feed.NewFeedPageState(time.Now().UTC().Add(-feed.FeedSessionTTL * 2))
	cursor := (&feed.FeedCursor{State: state}).Encode()
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
