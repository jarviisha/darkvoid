package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

type mockFeedEventOutbox struct {
	createdCalls int
	deletedCalls int
	changedCalls int
	err          error
	committed    bool
}

func (m *mockFeedEventOutbox) EnqueuePostCreated(_ context.Context, tx pgx.Tx, _, _ uuid.UUID, _ string, _ time.Time) error {
	m.createdCalls++
	if recording, ok := tx.(*recordingTx); ok {
		m.committed = recording.committed
	}
	return m.err
}

func (m *mockFeedEventOutbox) EnqueuePostDeleted(_ context.Context, _ pgx.Tx, _, _ uuid.UUID) error {
	m.deletedCalls++
	return m.err
}

func (m *mockFeedEventOutbox) EnqueuePostVisibilityChanged(_ context.Context, _ pgx.Tx, _, _ uuid.UUID, _ string, _ time.Time) error {
	m.changedCalls++
	return m.err
}

type recordingTx struct {
	*mockTx
	committed bool
}

func (tx *recordingTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

type recordingTxBeginner struct{ tx *recordingTx }

func (b *recordingTxBeginner) Begin(context.Context) (pgx.Tx, error) { return b.tx, nil }

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

func TestCreatePost_PersistsFeedOutboxInsideTransaction(t *testing.T) {
	tx := &recordingTx{mockTx: &mockTx{}}
	outbox := &mockFeedEventOutbox{}
	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, &mockLikeRepo{})
	svc.pool = &recordingTxBeginner{tx: tx}
	svc.feedOutbox = outbox
	if _, err := svc.CreatePost(context.Background(), uuid.New(), "Hello world", entity.VisibilityPublic, nil, nil, nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if outbox.createdCalls != 1 || outbox.committed {
		t.Fatalf("outbox calls = %d, observed committed=%v; want one pre-commit enqueue", outbox.createdCalls, outbox.committed)
	}
	if !tx.committed {
		t.Fatal("post transaction was not committed")
	}
}

func TestCreatePost_OutboxFailureAbortsMutation(t *testing.T) {
	tx := &recordingTx{mockTx: &mockTx{}}
	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, &mockLikeRepo{})
	svc.pool = &recordingTxBeginner{tx: tx}
	svc.feedOutbox = &mockFeedEventOutbox{err: errors.New("outbox unavailable")}
	if _, err := svc.CreatePost(context.Background(), uuid.New(), "Hello world", entity.VisibilityPublic, nil, nil, nil); err == nil {
		t.Fatal("expected outbox failure")
	}
	if tx.committed {
		t.Fatal("post transaction committed after outbox failure")
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

func TestUpdatePost_InvalidatesTrending(t *testing.T) {
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
	inv := &mockTrendingInvalidator{}
	svc.trendingInvalidator = inv
	if _, err := svc.UpdatePost(context.Background(), postID, authorID, "updated", entity.VisibilityPrivate, nil, nil); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if inv.calls != 1 {
		t.Errorf("expected trending invalidated once, got %d calls", inv.calls)
	}
}

func TestUpdatePost_TrendingInvalidatorFailureDoesNotFailUpdate(t *testing.T) {
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
	svc.trendingInvalidator = &mockTrendingInvalidator{err: pkgerrors.ErrNotFound}
	if _, err := svc.UpdatePost(context.Background(), postID, authorID, "updated", entity.VisibilityPublic, nil, nil); err != nil {
		t.Fatalf("update must succeed despite invalidator failure, got %v", err)
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

func TestDeletePost_InvalidatesTrending(t *testing.T) {
	authorID := uuid.New()
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(authorID), nil
		},
		delete: func(_ context.Context, _ uuid.UUID) error { return nil },
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})
	inv := &mockTrendingInvalidator{}
	svc.trendingInvalidator = inv
	if err := svc.DeletePost(context.Background(), uuid.New(), authorID); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if inv.calls != 1 {
		t.Errorf("expected trending invalidated once, got %d calls", inv.calls)
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
		getByAuthorWithCursor: func(_ context.Context, _ uuid.UUID, _ pgtype.Timestamptz, _ uuid.UUID, _ []string, _ int32) ([]*entity.Post, error) {
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
		getByAuthorWithCursor: func(_ context.Context, _ uuid.UUID, _ pgtype.Timestamptz, _ uuid.UUID, _ []string, _ int32) ([]*entity.Post, error) {
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
		getByAuthorWithCursor: func(_ context.Context, _ uuid.UUID, _ pgtype.Timestamptz, _ uuid.UUID, _ []string, _ int32) ([]*entity.Post, error) {
			return nil, pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	_, _, err := svc.GetUserPosts(context.Background(), uuid.New(), nil, nil, "", 20)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// The response lists mentions in the order the author wrote them. Reading them
// back cannot give that: the batch query has no ORDER BY, and every row of one
// transaction shares its created_at.
func TestCreatePost_MentionsInWrittenOrder_WithoutReadBack(t *testing.T) {
	u1, u2, u3 := uuid.New(), uuid.New(), uuid.New()
	mr := &mockMentionRepo{
		getBatch: func(context.Context, []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
			t.Error("mentions read back after create")
			return nil, nil
		},
	}
	users := &mockUserReader{
		getAuthorsByIDs: func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			m := make(map[uuid.UUID]*entity.Author, len(ids))
			for _, id := range ids {
				m[id] = &entity.Author{ID: id, Username: id.String()}
			}
			return m, nil
		},
	}
	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, &mockLikeRepo{})
	svc.mentionRepo = mr
	svc.hydrator = NewHydrator(HydratorDeps{Users: users, Mentions: mr})

	p, err := svc.CreatePost(context.Background(), uuid.New(), "hi", entity.VisibilityPublic, nil, []uuid.UUID{u3, u1, u2}, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(p.Mentions) != 3 || p.Mentions[0].ID != u3 || p.Mentions[1].ID != u1 || p.Mentions[2].ID != u2 {
		t.Errorf("mentions = %v, want [u3 u1 u2]", p.Mentions)
	}
}

// The updated post comes back hydrated as its author sees it: author, mentions
// read from what was stored, and the author's own liked flag.
func TestUpdatePost_ReturnsHydratedPost(t *testing.T) {
	authorID, postID, mentioned := uuid.New(), uuid.New(), uuid.New()
	pr := &mockPostRepo{
		getByID: func(context.Context, uuid.UUID) (*entity.Post, error) {
			p := samplePost(authorID)
			p.ID = postID
			return p, nil
		},
		update: func(_ context.Context, _ uuid.UUID, content string, v entity.Visibility) (*entity.Post, error) {
			return &entity.Post{ID: postID, AuthorID: authorID, Content: content, Visibility: v}, nil
		},
	}
	lr := &mockLikeRepo{getLikedPostIDs: func(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) { return ids, nil }}
	mr := &mockMentionRepo{getBatch: func(context.Context, []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
		return map[uuid.UUID][]uuid.UUID{postID: {mentioned}}, nil
	}}
	users := &mockUserReader{getAuthorsByIDs: func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
		m := make(map[uuid.UUID]*entity.Author, len(ids))
		for _, id := range ids {
			m[id] = &entity.Author{ID: id}
		}
		return m, nil
	}}
	svc := newPostService(pr, &mockMediaRepo{}, lr)
	svc.mentionRepo = mr
	svc.hydrator = NewHydrator(HydratorDeps{Likes: lr, Users: users, Mentions: mr})

	p, err := svc.UpdatePost(context.Background(), postID, authorID, "edited", entity.VisibilityPublic, []uuid.UUID{mentioned}, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p.Author == nil || p.Author.ID != authorID {
		t.Errorf("author = %+v, want %s", p.Author, authorID)
	}
	if len(p.Mentions) != 1 || p.Mentions[0].ID != mentioned {
		t.Errorf("mentions = %v, want [%s]", p.Mentions, mentioned)
	}
	if !p.IsLiked {
		t.Error("expected IsLiked for the author's own like")
	}
}
