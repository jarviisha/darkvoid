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
)

// --------------------------------------------------------------------------
// Mock: commentLikeService
// --------------------------------------------------------------------------

type mockCommentLikeService struct {
	toggle func(ctx context.Context, userID, commentID uuid.UUID) (bool, error)
}

func (m *mockCommentLikeService) Toggle(ctx context.Context, userID, commentID uuid.UUID) (bool, error) {
	if m.toggle != nil {
		return m.toggle(ctx, userID, commentID)
	}
	return true, nil
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

func newCommentLikeHandler(svc commentLikeService) *CommentLikeHandler {
	return &CommentLikeHandler{commentLikeService: svc}
}

func assertCommentLiked(t *testing.T, w *httptest.ResponseRecorder, want bool) {
	t.Helper()
	var body map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if got := body["liked"]; got != want {
		t.Errorf("liked = %v, want %v", got, want)
	}
}

// --------------------------------------------------------------------------
// ToggleCommentLike tests
// --------------------------------------------------------------------------

func TestToggleCommentLike_Success_Liked(t *testing.T) {
	userID := uuid.New()
	commentID := uuid.New()
	svc := &mockCommentLikeService{
		toggle: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
	}
	h := newCommentLikeHandler(svc)

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	r = withAuth(r, userID)
	r = withChiParam(r, "commentID", commentID.String())
	w := httptest.NewRecorder()

	h.ToggleCommentLike(w, r)
	assertStatus(t, w, http.StatusOK)
	assertCommentLiked(t, w, true)
}

func TestToggleCommentLike_Success_Unliked(t *testing.T) {
	userID := uuid.New()
	commentID := uuid.New()
	svc := &mockCommentLikeService{
		toggle: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
	}
	h := newCommentLikeHandler(svc)

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	r = withAuth(r, userID)
	r = withChiParam(r, "commentID", commentID.String())
	w := httptest.NewRecorder()

	h.ToggleCommentLike(w, r)
	assertStatus(t, w, http.StatusOK)
	assertCommentLiked(t, w, false)
}

func TestToggleCommentLike_Unauthenticated(t *testing.T) {
	commentID := uuid.New()
	h := newCommentLikeHandler(&mockCommentLikeService{})

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	r = withChiParam(r, "commentID", commentID.String())
	w := httptest.NewRecorder()

	h.ToggleCommentLike(w, r)
	assertStatus(t, w, http.StatusUnauthorized)
}

func TestToggleCommentLike_InvalidCommentID(t *testing.T) {
	userID := uuid.New()
	h := newCommentLikeHandler(&mockCommentLikeService{})

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	r = withAuth(r, userID)
	r = withChiParam(r, "commentID", "bad")
	w := httptest.NewRecorder()

	h.ToggleCommentLike(w, r)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestToggleCommentLike_CommentNotFound(t *testing.T) {
	userID := uuid.New()
	commentID := uuid.New()
	svc := &mockCommentLikeService{
		toggle: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, post.ErrCommentNotFound
		},
	}
	h := newCommentLikeHandler(svc)

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	r = withAuth(r, userID)
	r = withChiParam(r, "commentID", commentID.String())
	w := httptest.NewRecorder()

	h.ToggleCommentLike(w, r)
	assertStatus(t, w, http.StatusNotFound)
	assertErrCode(t, w, "COMMENT_NOT_FOUND")
}

func TestToggleCommentLike_SelfLike(t *testing.T) {
	userID := uuid.New()
	commentID := uuid.New()
	svc := &mockCommentLikeService{
		toggle: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, post.ErrSelfLike
		},
	}
	h := newCommentLikeHandler(svc)

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	r = withAuth(r, userID)
	r = withChiParam(r, "commentID", commentID.String())
	w := httptest.NewRecorder()

	h.ToggleCommentLike(w, r)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrCode(t, w, "SELF_LIKE")
}

func TestToggleCommentLike_ServiceError(t *testing.T) {
	userID := uuid.New()
	commentID := uuid.New()
	svc := &mockCommentLikeService{
		toggle: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, fmt.Errorf("unexpected db failure")
		},
	}
	h := newCommentLikeHandler(svc)

	r, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	r = withAuth(r, userID)
	r = withChiParam(r, "commentID", commentID.String())
	w := httptest.NewRecorder()

	h.ToggleCommentLike(w, r)
	assertStatus(t, w, http.StatusInternalServerError)
}
