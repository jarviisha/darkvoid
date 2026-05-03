package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	post "github.com/jarviisha/darkvoid/internal/feature/post"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	pkgerrors "github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// Test helpers
// --------------------------------------------------------------------------
// Note: Shared mocks and helper functions have been moved to post_test_helpers.go

func assertErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", code)
	}
	sentinels := map[string]error{
		"POST_NOT_FOUND":     post.ErrPostNotFound,
		"COMMENT_NOT_FOUND":  post.ErrCommentNotFound,
		"FORBIDDEN":          post.ErrForbidden,
		"SELF_LIKE":          post.ErrSelfLike,
		"EMPTY_CONTENT":      post.ErrEmptyContent,
		"INVALID_VISIBILITY": post.ErrInvalidVisibility,
	}
	if want, ok := sentinels[code]; ok {
		if err != want {
			t.Errorf("expected sentinel %q, got: %v", code, err)
		}
		return
	}
	t.Errorf("unknown sentinel code %q", code)
}

// --------------------------------------------------------------------------
// CreatePost tests
// --------------------------------------------------------------------------

type mockFeedEventEmitter struct {
	postID     uuid.UUID
	authorID   uuid.UUID
	visibility string
	called     int
	err        error
}

func (m *mockFeedEventEmitter) EmitPostCreated(_ context.Context, postID, authorID uuid.UUID, visibility string, _ time.Time) error {
	m.called++
	m.postID = postID
	m.authorID = authorID
	m.visibility = visibility
	return m.err
}

func TestCreatePost_Success(t *testing.T) {
	authorID := uuid.New()
	pr := &mockPostRepo{}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	p, err := svc.CreatePost(context.Background(), authorID, "Hello world", entity.VisibilityPublic, nil, nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p.AuthorID != authorID {
		t.Errorf("expected authorID %v, got %v", authorID, p.AuthorID)
	}
	if p.Content != "Hello world" {
		t.Errorf("expected content 'Hello world', got %q", p.Content)
	}
}

func TestCreatePost_EmitsFeedEventAfterSuccess(t *testing.T) {
	authorID := uuid.New()
	emitter := &mockFeedEventEmitter{}
	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, &mockLikeRepo{})
	svc.WithFeedEventEmitter(emitter)

	p, err := svc.CreatePost(context.Background(), authorID, "Hello world", entity.VisibilityPublic, nil, nil, nil)
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if emitter.called != 1 || emitter.postID != p.ID || emitter.authorID != authorID || emitter.visibility != string(entity.VisibilityPublic) {
		t.Fatalf("feed event mismatch: %+v post=%+v", emitter, p)
	}
}

func TestCreatePost_DoesNotEmitFeedEventOnCreateFailure(t *testing.T) {
	emitter := &mockFeedEventEmitter{}
	svc := newPostService(&mockPostRepo{
		create: func(_ context.Context, _ uuid.UUID, _ string, _ entity.Visibility) (*entity.Post, error) {
			return nil, errors.New("db down")
		},
	}, &mockMediaRepo{}, &mockLikeRepo{})
	svc.WithFeedEventEmitter(emitter)

	_, err := svc.CreatePost(context.Background(), uuid.New(), "Hello world", entity.VisibilityPublic, nil, nil, nil)
	if err == nil {
		t.Fatal("expected create error")
	}
	if emitter.called != 0 {
		t.Fatalf("feed event emitted on failure: %d", emitter.called)
	}
}

func TestCreatePost_FeedEmitterFailureIsNonFatal(t *testing.T) {
	emitter := &mockFeedEventEmitter{err: errors.New("queue full")}
	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, &mockLikeRepo{})
	svc.WithFeedEventEmitter(emitter)

	if _, err := svc.CreatePost(context.Background(), uuid.New(), "Hello world", entity.VisibilityPublic, nil, nil, nil); err != nil {
		t.Fatalf("CreatePost should ignore feed emitter error: %v", err)
	}
	if emitter.called != 1 {
		t.Fatalf("feed emitter calls = %d, want 1", emitter.called)
	}
}

func TestCreatePost_EmptyContentAndNoMedia(t *testing.T) {
	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, &mockLikeRepo{})

	_, err := svc.CreatePost(context.Background(), uuid.New(), "   ", entity.VisibilityPublic, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertErrorCode(t, err, "EMPTY_CONTENT")
}

func TestCreatePost_WhitespaceOnlyContent_WithMedia_Succeeds(t *testing.T) {
	// Media present → allowed even with empty content
	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, &mockLikeRepo{})

	p, err := svc.CreatePost(context.Background(), uuid.New(), "   ", entity.VisibilityPublic, []string{"media/img.jpg"}, nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(p.Media) != 1 {
		t.Errorf("expected 1 media, got %d", len(p.Media))
	}
}

func TestCreatePost_InvalidVisibility(t *testing.T) {
	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, &mockLikeRepo{})

	_, err := svc.CreatePost(context.Background(), uuid.New(), "content", "invalid", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertErrorCode(t, err, "INVALID_VISIBILITY")
}

func TestCreatePost_ContentTrimmed(t *testing.T) {
	var savedContent string
	pr := &mockPostRepo{
		create: func(_ context.Context, _ uuid.UUID, content string, _ entity.Visibility) (*entity.Post, error) {
			savedContent = content
			return &entity.Post{ID: uuid.New(), Content: content, CreatedAt: time.Now()}, nil
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	_, err := svc.CreatePost(context.Background(), uuid.New(), "  trimmed  ", entity.VisibilityPublic, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if savedContent != "trimmed" {
		t.Errorf("expected content to be trimmed, got %q", savedContent)
	}
}

func TestCreatePost_AttachesMedia(t *testing.T) {
	mediaAdded := 0
	mr := &mockMediaRepo{
		add: func(_ context.Context, _ uuid.UUID, key, _ string, _ int32) (*entity.PostMedia, error) {
			mediaAdded++
			return &entity.PostMedia{ID: uuid.New(), MediaKey: key}, nil
		},
	}
	svc := newPostService(&mockPostRepo{}, mr, &mockLikeRepo{})

	p, err := svc.CreatePost(context.Background(), uuid.New(), "post with media", entity.VisibilityPublic, []string{"img1.jpg", "img2.jpg"}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mediaAdded != 2 {
		t.Errorf("expected 2 media adds, got %d", mediaAdded)
	}
	if len(p.Media) != 2 {
		t.Errorf("expected 2 media on post, got %d", len(p.Media))
	}
}

func TestCreatePost_InferMediaType_Video(t *testing.T) {
	var savedType string
	mr := &mockMediaRepo{
		add: func(_ context.Context, _ uuid.UUID, _, mediaType string, _ int32) (*entity.PostMedia, error) {
			savedType = mediaType
			return &entity.PostMedia{ID: uuid.New()}, nil
		},
	}
	svc := newPostService(&mockPostRepo{}, mr, &mockLikeRepo{})

	if _, err := svc.CreatePost(context.Background(), uuid.New(), "video post", entity.VisibilityPublic, []string{"clip.mp4"}, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if savedType != "video" {
		t.Errorf("expected media type 'video' for .mp4, got %q", savedType)
	}
}

func TestCreatePost_InferMediaType_Image(t *testing.T) {
	var savedType string
	mr := &mockMediaRepo{
		add: func(_ context.Context, _ uuid.UUID, _, mediaType string, _ int32) (*entity.PostMedia, error) {
			savedType = mediaType
			return &entity.PostMedia{ID: uuid.New()}, nil
		},
	}
	svc := newPostService(&mockPostRepo{}, mr, &mockLikeRepo{})

	if _, err := svc.CreatePost(context.Background(), uuid.New(), "image post", entity.VisibilityPublic, []string{"photo.jpg"}, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if savedType != "image" {
		t.Errorf("expected media type 'image' for .jpg, got %q", savedType)
	}
}

func TestCreatePost_RepoCreateError(t *testing.T) {
	pr := &mockPostRepo{
		create: func(_ context.Context, _ uuid.UUID, _ string, _ entity.Visibility) (*entity.Post, error) {
			return nil, pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	_, err := svc.CreatePost(context.Background(), uuid.New(), "content", entity.VisibilityPublic, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error from repo create, got nil")
	}
}

// --------------------------------------------------------------------------
// GetPost tests
// --------------------------------------------------------------------------

func TestGetPost_Success(t *testing.T) {
	authorID := uuid.New()
	postID := uuid.New()
	pr := &mockPostRepo{
		getByID: func(_ context.Context, id uuid.UUID) (*entity.Post, error) {
			return samplePost(authorID), nil
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	p, err := svc.GetPost(context.Background(), postID, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p == nil {
		t.Fatal("expected post, got nil")
	}
}

func TestGetPost_NotFound(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	_, err := svc.GetPost(context.Background(), uuid.New(), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertErrorCode(t, err, "POST_NOT_FOUND")
}

func TestGetPost_IsLiked_WhenViewerProvided(t *testing.T) {
	viewerID := uuid.New()
	postID := uuid.New()
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			p := samplePost(uuid.New())
			p.ID = postID
			p.LikeCount = 5
			return p, nil
		},
	}
	lr := &mockLikeRepo{
		getLikedPostIDs: func(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
			return ids, nil // viewer has liked all posts
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, lr)

	p, err := svc.GetPost(context.Background(), postID, &viewerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !p.IsLiked {
		t.Error("expected IsLiked=true when viewer has liked")
	}
	if p.LikeCount != 5 {
		t.Errorf("expected LikeCount=5, got %d", p.LikeCount)
	}
}

func TestGetPost_IsLiked_NilWhenNoViewer(t *testing.T) {
	getLikedCalled := false
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(uuid.New()), nil
		},
	}
	lr := &mockLikeRepo{
		getLikedPostIDs: func(_ context.Context, _ uuid.UUID, _ []uuid.UUID) ([]uuid.UUID, error) {
			getLikedCalled = true
			return nil, nil
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, lr)

	if _, err := svc.GetPost(context.Background(), uuid.New(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if getLikedCalled {
		t.Error("GetLikedPostIDs should not be called when viewerID is nil")
	}
}

// --------------------------------------------------------------------------
// UpdatePost tests
// --------------------------------------------------------------------------

func TestUpdatePost_Success(t *testing.T) {
	authorID := uuid.New()
	postID := uuid.New()
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			p := samplePost(authorID)
			p.ID = postID
			return p, nil
		},
		update: func(_ context.Context, _ uuid.UUID, content string, v entity.Visibility) (*entity.Post, error) {
			return &entity.Post{ID: postID, AuthorID: authorID, Content: content, Visibility: v, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	p, err := svc.UpdatePost(context.Background(), postID, authorID, "Updated content", entity.VisibilityFollowers, nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p.Content != "Updated content" {
		t.Errorf("expected content 'Updated content', got %q", p.Content)
	}
}

func TestUpdatePost_NotFound(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	_, err := svc.UpdatePost(context.Background(), uuid.New(), uuid.New(), "content", entity.VisibilityPublic, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertErrorCode(t, err, "POST_NOT_FOUND")
}

func TestUpdatePost_Forbidden_NotOwner(t *testing.T) {
	authorID := uuid.New()
	otherUserID := uuid.New()
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(authorID), nil
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	_, err := svc.UpdatePost(context.Background(), uuid.New(), otherUserID, "content", entity.VisibilityPublic, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertErrorCode(t, err, "FORBIDDEN")
}

func TestUpdatePost_InvalidVisibility(t *testing.T) {
	authorID := uuid.New()
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(authorID), nil
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	_, err := svc.UpdatePost(context.Background(), uuid.New(), authorID, "content", "bad", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertErrorCode(t, err, "INVALID_VISIBILITY")
}

// --------------------------------------------------------------------------
// DeletePost tests
// --------------------------------------------------------------------------

func TestDeletePost_Success(t *testing.T) {
	authorID := uuid.New()
	deleteCalled := false
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(authorID), nil
		},
		delete: func(_ context.Context, _ uuid.UUID) error {
			deleteCalled = true
			return nil
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	err := svc.DeletePost(context.Background(), uuid.New(), authorID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !deleteCalled {
		t.Error("expected Delete to be called")
	}
}

func TestDeletePost_Forbidden_NotOwner(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(uuid.New()), nil
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	err := svc.DeletePost(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertErrorCode(t, err, "FORBIDDEN")
}

func TestDeletePost_NotFound(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	err := svc.DeletePost(context.Background(), uuid.New(), uuid.New())
	assertErrorCode(t, err, "POST_NOT_FOUND")
}

func TestDeletePost_RepoError(t *testing.T) {
	authorID := uuid.New()
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(authorID), nil
		},
		delete: func(_ context.Context, _ uuid.UUID) error {
			return pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	err := svc.DeletePost(context.Background(), uuid.New(), authorID)
	if err == nil {
		t.Fatal("expected error from repo delete, got nil")
	}
}

func TestDeletePost_GetByIDInternalError(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return nil, pkgerrors.NewInternalError(pkgerrors.ErrInternal) // not ErrNotFound
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	err := svc.DeletePost(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected internal error, got nil")
	}
}

func TestUpdatePost_RepoError(t *testing.T) {
	authorID := uuid.New()
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(authorID), nil
		},
		update: func(_ context.Context, _ uuid.UUID, _ string, _ entity.Visibility) (*entity.Post, error) {
			return nil, pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	_, err := svc.UpdatePost(context.Background(), uuid.New(), authorID, "new content", entity.VisibilityPublic, nil, nil)
	if err == nil {
		t.Fatal("expected error from repo update, got nil")
	}
}

func TestUpdatePost_GetByIDInternalError(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return nil, pkgerrors.NewInternalError(pkgerrors.ErrInternal) // not ErrNotFound
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	_, err := svc.UpdatePost(context.Background(), uuid.New(), uuid.New(), "content", entity.VisibilityPublic, nil, nil)
	if err == nil {
		t.Fatal("expected internal error, got nil")
	}
}

// --------------------------------------------------------------------------
// GetUserPosts tests
// --------------------------------------------------------------------------

func TestGetUserPosts_Success(t *testing.T) {
	authorID := uuid.New()
	pr := &mockPostRepo{
		getByAuthorWithCursor: func(_ context.Context, _ uuid.UUID, _ pgtype.Timestamptz, _ uuid.UUID, _ string, _ int32) ([]*entity.Post, error) {
			return []*entity.Post{samplePost(authorID)}, nil
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	posts, nextCursor, err := svc.GetUserPosts(context.Background(), authorID, nil, nil, "", 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(posts) != 1 {
		t.Errorf("expected 1 post, got %d", len(posts))
	}
	if nextCursor != nil {
		t.Errorf("expected no next cursor for single page, got %v", nextCursor)
	}
}

func TestGetUserPosts_NextPageCursor(t *testing.T) {
	authorID := uuid.New()
	// Return limit+1 posts to trigger next-page cursor generation
	pr := &mockPostRepo{
		getByAuthorWithCursor: func(_ context.Context, _ uuid.UUID, _ pgtype.Timestamptz, _ uuid.UUID, _ string, _ int32) ([]*entity.Post, error) {
			posts := make([]*entity.Post, 3) // limit=2, returns 3 → next page
			for i := range posts {
				posts[i] = samplePost(authorID)
			}
			return posts, nil
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	posts, nextCursor, err := svc.GetUserPosts(context.Background(), authorID, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(posts) != 2 {
		t.Errorf("expected 2 posts (trimmed), got %d", len(posts))
	}
	if nextCursor == nil {
		t.Error("expected non-nil next cursor when more pages exist")
	}
}

func TestGetUserPosts_InvalidCursorPostID(t *testing.T) {
	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, &mockLikeRepo{})
	cursor := &post.UserPostCursor{CreatedAt: time.Now(), PostID: "not-a-uuid"}

	_, _, err := svc.GetUserPosts(context.Background(), uuid.New(), nil, cursor, "", 20)
	if err == nil {
		t.Fatal("expected error for invalid cursor post ID, got nil")
	}
}

func TestGetUserPosts_RepoError(t *testing.T) {
	pr := &mockPostRepo{
		getByAuthorWithCursor: func(_ context.Context, _ uuid.UUID, _ pgtype.Timestamptz, _ uuid.UUID, _ string, _ int32) ([]*entity.Post, error) {
			return nil, pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	_, _, err := svc.GetUserPosts(context.Background(), uuid.New(), nil, nil, "", 20)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
