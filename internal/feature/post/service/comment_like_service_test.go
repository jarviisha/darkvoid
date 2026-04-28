package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	post "github.com/jarviisha/darkvoid/internal/feature/post"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	"github.com/jarviisha/darkvoid/internal/pagination"
	pkgerrors "github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// Mock: commentRepo
// --------------------------------------------------------------------------

type mockCommentRepo struct {
	create              func(ctx context.Context, postID, authorID uuid.UUID, parentID *uuid.UUID, content string) (*entity.Comment, error)
	getByID             func(ctx context.Context, id uuid.UUID) (*entity.Comment, error)
	getByPost           func(ctx context.Context, postID uuid.UUID, limit, offset int32) ([]*entity.Comment, error)
	countByPost         func(ctx context.Context, postID uuid.UUID) (int64, error)
	getReplies          func(ctx context.Context, parentID uuid.UUID, limit, offset int32) ([]*entity.Comment, error)
	getRepliesPreview   func(ctx context.Context, parentIDs []uuid.UUID, limitPerParent int32) (map[uuid.UUID][]*entity.Comment, error)
	getReplyCountsBatch func(ctx context.Context, parentIDs []uuid.UUID) (map[uuid.UUID]int64, error)
	delete              func(ctx context.Context, id uuid.UUID) error
}

func (m *mockCommentRepo) WithTx(_ pgx.Tx) commentRepo { return m }
func (m *mockCommentRepo) Create(ctx context.Context, postID, authorID uuid.UUID, parentID *uuid.UUID, content string) (*entity.Comment, error) {
	if m.create != nil {
		return m.create(ctx, postID, authorID, parentID, content)
	}
	return &entity.Comment{ID: uuid.New(), PostID: postID, AuthorID: authorID, Content: content, CreatedAt: time.Now()}, nil
}
func (m *mockCommentRepo) GetByID(ctx context.Context, id uuid.UUID) (*entity.Comment, error) {
	if m.getByID != nil {
		return m.getByID(ctx, id)
	}
	return nil, pkgerrors.ErrNotFound
}
func (m *mockCommentRepo) GetByPost(ctx context.Context, postID uuid.UUID, limit, offset int32) ([]*entity.Comment, error) {
	if m.getByPost != nil {
		return m.getByPost(ctx, postID, limit, offset)
	}
	return nil, nil
}
func (m *mockCommentRepo) CountByPost(ctx context.Context, postID uuid.UUID) (int64, error) {
	if m.countByPost != nil {
		return m.countByPost(ctx, postID)
	}
	return 0, nil
}
func (m *mockCommentRepo) GetReplies(ctx context.Context, parentID uuid.UUID, limit, offset int32) ([]*entity.Comment, error) {
	if m.getReplies != nil {
		return m.getReplies(ctx, parentID, limit, offset)
	}
	return nil, nil
}
func (m *mockCommentRepo) GetRepliesPreview(ctx context.Context, parentIDs []uuid.UUID, limitPerParent int32) (map[uuid.UUID][]*entity.Comment, error) {
	if m.getRepliesPreview != nil {
		return m.getRepliesPreview(ctx, parentIDs, limitPerParent)
	}
	return map[uuid.UUID][]*entity.Comment{}, nil
}
func (m *mockCommentRepo) GetReplyCountsBatch(ctx context.Context, parentIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	if m.getReplyCountsBatch != nil {
		return m.getReplyCountsBatch(ctx, parentIDs)
	}
	return map[uuid.UUID]int64{}, nil
}
func (m *mockCommentRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if m.delete != nil {
		return m.delete(ctx, id)
	}
	return nil
}

// --------------------------------------------------------------------------
// mockCommentMediaRepo
// --------------------------------------------------------------------------

type mockCommentMediaRepo struct{}

func (m *mockCommentMediaRepo) WithTx(_ pgx.Tx) commentMediaRepo { return m }
func (m *mockCommentMediaRepo) Add(_ context.Context, commentID uuid.UUID, mediaKey, _ string, _ int32) (*entity.CommentMedia, error) {
	return &entity.CommentMedia{ID: uuid.New(), CommentID: commentID, MediaKey: mediaKey}, nil
}
func (m *mockCommentMediaRepo) GetByCommentsBatch(_ context.Context, _ []uuid.UUID) (map[uuid.UUID][]*entity.CommentMedia, error) {
	return map[uuid.UUID][]*entity.CommentMedia{}, nil
}

// --------------------------------------------------------------------------
// mockCommentMentionRepo
// --------------------------------------------------------------------------

type mockCommentMentionRepo struct {
	insert   func(ctx context.Context, commentID, userID uuid.UUID) error
	getBatch func(ctx context.Context, commentIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
}

func (m *mockCommentMentionRepo) Insert(ctx context.Context, commentID, userID uuid.UUID) error {
	if m.insert != nil {
		return m.insert(ctx, commentID, userID)
	}
	return nil
}
func (m *mockCommentMentionRepo) GetBatch(ctx context.Context, commentIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	if m.getBatch != nil {
		return m.getBatch(ctx, commentIDs)
	}
	return map[uuid.UUID][]uuid.UUID{}, nil
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

func newCommentService(cr commentRepo, pr postRepo) *CommentService {
	return &CommentService{pool: &mockTxBeginner{}, commentRepo: cr, commentMediaRepo: &mockCommentMediaRepo{}, postRepo: pr}
}

func newLikeService(lr likeRepo, pr postRepo) *LikeService {
	return &LikeService{likeRepo: lr, postRepo: pr}
}

func postExists(_ uuid.UUID) *mockPostRepo {
	return &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(uuid.New()), nil
		},
	}
}

// commentExists returns a mockCommentRepo whose GetByID returns a comment with the given authorID.
func commentExists(authorID uuid.UUID) *mockCommentRepo {
	return &mockCommentRepo{
		getByID: func(_ context.Context, id uuid.UUID) (*entity.Comment, error) {
			return &entity.Comment{ID: id, AuthorID: authorID}, nil
		},
	}
}

// --------------------------------------------------------------------------
// CommentService — CreateComment tests
// --------------------------------------------------------------------------

func TestCreateComment_Success(t *testing.T) {
	postID := uuid.New()
	authorID := uuid.New()
	pr := postExists(postID)
	svc := newCommentService(&mockCommentRepo{}, pr)

	c, err := svc.CreateComment(context.Background(), postID, authorID, nil, "Great post!", nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if c.Content != "Great post!" {
		t.Errorf("expected content 'Great post!', got %q", c.Content)
	}
}

func TestCreateComment_EmptyContent(t *testing.T) {
	svc := newCommentService(&mockCommentRepo{}, postExists(uuid.New()))

	_, err := svc.CreateComment(context.Background(), uuid.New(), uuid.New(), nil, "   ", nil, nil)
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
}

func TestCreateComment_PostNotFound(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}
	svc := newCommentService(&mockCommentRepo{}, pr)

	_, err := svc.CreateComment(context.Background(), uuid.New(), uuid.New(), nil, "content", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrPostNotFound {
		t.Errorf("expected ErrPostNotFound, got %v", err)
	}
}

func TestCreateComment_Reply_Success(t *testing.T) {
	postID := uuid.New()
	parentID := uuid.New()
	pr := postExists(postID)
	cr := &mockCommentRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Comment, error) {
			return &entity.Comment{ID: parentID, PostID: postID}, nil
		},
	}
	svc := newCommentService(cr, pr)

	c, err := svc.CreateComment(context.Background(), postID, uuid.New(), &parentID, "Reply!", nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if c == nil {
		t.Fatal("expected comment, got nil")
	}
}

func TestCreateComment_Reply_ParentNotFound(t *testing.T) {
	postID := uuid.New()
	parentID := uuid.New()
	pr := postExists(postID)
	cr := &mockCommentRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Comment, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}
	svc := newCommentService(cr, pr)

	_, err := svc.CreateComment(context.Background(), postID, uuid.New(), &parentID, "Reply!", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrCommentNotFound {
		t.Errorf("expected ErrCommentNotFound, got %v", err)
	}
}

func TestCreateComment_Reply_ParentBelongsToDifferentPost(t *testing.T) {
	postID := uuid.New()
	parentID := uuid.New()
	differentPostID := uuid.New()
	pr := postExists(postID)
	cr := &mockCommentRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Comment, error) {
			// parent belongs to a different post
			return &entity.Comment{ID: parentID, PostID: differentPostID}, nil
		},
	}
	svc := newCommentService(cr, pr)

	_, err := svc.CreateComment(context.Background(), postID, uuid.New(), &parentID, "Reply!", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrCommentNotFound {
		t.Errorf("expected ErrCommentNotFound, got %v", err)
	}
}

// --------------------------------------------------------------------------
// CommentService — GetComments tests
// --------------------------------------------------------------------------

func TestGetComments_Success(t *testing.T) {
	postID := uuid.New()
	pr := postExists(postID)
	cr := &mockCommentRepo{
		getByPost: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Comment, error) {
			return []*entity.Comment{
				{ID: uuid.New(), PostID: postID, Content: "c1"},
				{ID: uuid.New(), PostID: postID, Content: "c2"},
			}, nil
		},
		countByPost: func(_ context.Context, _ uuid.UUID) (int64, error) { return 2, nil },
	}
	svc := newCommentService(cr, pr)

	comments, pag, err := svc.GetComments(context.Background(), postID, nil, pagination.PaginationRequest{Limit: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 2 {
		t.Errorf("expected 2 comments, got %d", len(comments))
	}
	if pag.Total != 2 {
		t.Errorf("expected total 2, got %d", pag.Total)
	}
}

func TestGetComments_PostNotFound(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}
	svc := newCommentService(&mockCommentRepo{}, pr)

	_, _, err := svc.GetComments(context.Background(), uuid.New(), nil, pagination.PaginationRequest{Limit: 20})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrPostNotFound {
		t.Errorf("expected ErrPostNotFound, got %v", err)
	}
}

// --------------------------------------------------------------------------
// CommentService — GetReplies tests
// --------------------------------------------------------------------------

func TestGetReplies_Success(t *testing.T) {
	commentID := uuid.New()
	cr := &mockCommentRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Comment, error) {
			return &entity.Comment{ID: commentID}, nil
		},
		getReplies: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Comment, error) {
			return []*entity.Comment{
				{ID: uuid.New(), ParentID: &commentID},
			}, nil
		},
	}
	svc := newCommentService(cr, &mockPostRepo{})

	replies, _, err := svc.GetReplies(context.Background(), commentID, nil, pagination.PaginationRequest{Limit: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(replies) != 1 {
		t.Errorf("expected 1 reply, got %d", len(replies))
	}
}

func TestGetReplies_CommentNotFound(t *testing.T) {
	cr := &mockCommentRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Comment, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}
	svc := newCommentService(cr, &mockPostRepo{})

	_, _, err := svc.GetReplies(context.Background(), uuid.New(), nil, pagination.PaginationRequest{Limit: 20})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrCommentNotFound {
		t.Errorf("expected ErrCommentNotFound, got %v", err)
	}
}

// --------------------------------------------------------------------------
// CommentService — DeleteComment tests
// --------------------------------------------------------------------------

func TestDeleteComment_Success(t *testing.T) {
	authorID := uuid.New()
	commentID := uuid.New()
	deleteCalled := false
	cr := &mockCommentRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Comment, error) {
			return &entity.Comment{ID: commentID, AuthorID: authorID}, nil
		},
		delete: func(_ context.Context, _ uuid.UUID) error {
			deleteCalled = true
			return nil
		},
	}
	svc := newCommentService(cr, &mockPostRepo{})

	err := svc.DeleteComment(context.Background(), commentID, authorID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !deleteCalled {
		t.Error("expected Delete to be called")
	}
}

func TestDeleteComment_NotFound(t *testing.T) {
	cr := &mockCommentRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Comment, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}
	svc := newCommentService(cr, &mockPostRepo{})

	err := svc.DeleteComment(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrCommentNotFound {
		t.Errorf("expected ErrCommentNotFound, got %v", err)
	}
}

func TestDeleteComment_Forbidden_NotOwner(t *testing.T) {
	authorID := uuid.New()
	otherUserID := uuid.New()
	cr := &mockCommentRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Comment, error) {
			return &entity.Comment{ID: uuid.New(), AuthorID: authorID}, nil
		},
	}
	svc := newCommentService(cr, &mockPostRepo{})

	err := svc.DeleteComment(context.Background(), uuid.New(), otherUserID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrForbidden {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

// --------------------------------------------------------------------------
// LikeService — Like tests
// --------------------------------------------------------------------------

func TestLike_Success(t *testing.T) {
	authorID := uuid.New()
	viewerID := uuid.New() // different user
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(authorID), nil
		},
	}
	likeCalled := false
	lr := &mockLikeRepo{
		like: func(_ context.Context, _, _ uuid.UUID) error {
			likeCalled = true
			return nil
		},
	}
	svc := newLikeService(lr, pr)

	err := svc.Like(context.Background(), viewerID, uuid.New())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !likeCalled {
		t.Error("expected Like to be called on repo")
	}
}

func TestLike_PostNotFound(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}
	svc := newLikeService(&mockLikeRepo{}, pr)

	err := svc.Like(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrPostNotFound {
		t.Errorf("expected ErrPostNotFound, got %v", err)
	}
}

func TestLike_SelfLike_Forbidden(t *testing.T) {
	userID := uuid.New()
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			// author is same as viewer
			return samplePost(userID), nil
		},
	}
	svc := newLikeService(&mockLikeRepo{}, pr)

	err := svc.Like(context.Background(), userID, uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrSelfLike {
		t.Errorf("expected ErrSelfLike, got %v", err)
	}
}

// --------------------------------------------------------------------------
// LikeService — Unlike tests
// --------------------------------------------------------------------------

func TestUnlike_Success(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(uuid.New()), nil
		},
	}
	unlikeCalled := false
	lr := &mockLikeRepo{
		unlike: func(_ context.Context, _, _ uuid.UUID) error {
			unlikeCalled = true
			return nil
		},
	}
	svc := newLikeService(lr, pr)

	err := svc.Unlike(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !unlikeCalled {
		t.Error("expected Unlike to be called on repo")
	}
}

func TestUnlike_PostNotFound(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}
	svc := newLikeService(&mockLikeRepo{}, pr)

	err := svc.Unlike(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrPostNotFound {
		t.Errorf("expected ErrPostNotFound, got %v", err)
	}
}

// --------------------------------------------------------------------------
// CommentLikeService — Toggle tests (T007)
// --------------------------------------------------------------------------

func TestToggleCommentLike_Like_WhenNotLiked(t *testing.T) {
	commentID := uuid.New()
	authorID := uuid.New()
	userID := uuid.New()
	var likeCalled bool
	clr := &mockCommentLikeRepo{
		isLiked: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
		like: func(_ context.Context, _, _ uuid.UUID) error {
			likeCalled = true
			return nil
		},
	}
	svc := newCommentLikeService(clr, commentExists(authorID))

	liked, err := svc.Toggle(context.Background(), userID, commentID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !liked {
		t.Error("expected liked=true")
	}
	if !likeCalled {
		t.Error("expected Like to be called")
	}
}

func TestToggleCommentLike_Unlike_WhenAlreadyLiked(t *testing.T) {
	commentID := uuid.New()
	authorID := uuid.New()
	userID := uuid.New()
	var unlikeCalled bool
	clr := &mockCommentLikeRepo{
		isLiked: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		unlike: func(_ context.Context, _, _ uuid.UUID) error {
			unlikeCalled = true
			return nil
		},
	}
	svc := newCommentLikeService(clr, commentExists(authorID))

	liked, err := svc.Toggle(context.Background(), userID, commentID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if liked {
		t.Error("expected liked=false after unlike")
	}
	if !unlikeCalled {
		t.Error("expected Unlike to be called")
	}
}

func TestToggleCommentLike_CommentNotFound(t *testing.T) {
	cr := &mockCommentRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Comment, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}
	svc := newCommentLikeService(&mockCommentLikeRepo{}, cr)

	_, err := svc.Toggle(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrCommentNotFound {
		t.Errorf("expected ErrCommentNotFound, got %v", err)
	}
}

func TestToggleCommentLike_SelfLike(t *testing.T) {
	userID := uuid.New()
	svc := newCommentLikeService(&mockCommentLikeRepo{}, commentExists(userID))

	_, err := svc.Toggle(context.Background(), userID, uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrSelfLike {
		t.Errorf("expected ErrSelfLike, got %v", err)
	}
}

func TestToggleCommentLike_IsLikedError(t *testing.T) {
	authorID := uuid.New()
	clr := &mockCommentLikeRepo{
		isLiked: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	svc := newCommentLikeService(clr, commentExists(authorID))

	_, err := svc.Toggle(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestToggleCommentLike_LikeRepoError(t *testing.T) {
	authorID := uuid.New()
	clr := &mockCommentLikeRepo{
		isLiked: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
		like: func(_ context.Context, _, _ uuid.UUID) error {
			return pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	svc := newCommentLikeService(clr, commentExists(authorID))

	_, err := svc.Toggle(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --------------------------------------------------------------------------
// LikeService — Toggle tests (T008)
// --------------------------------------------------------------------------

func TestToggle_Like_WhenNotLiked(t *testing.T) {
	authorID := uuid.New()
	userID := uuid.New()
	var likeCalled bool
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(authorID), nil
		},
	}
	lr := &mockLikeRepo{
		isLiked: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
		like: func(_ context.Context, _, _ uuid.UUID) error {
			likeCalled = true
			return nil
		},
	}
	svc := newLikeService(lr, pr)

	liked, err := svc.Toggle(context.Background(), userID, uuid.New())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !liked {
		t.Error("expected liked=true")
	}
	if !likeCalled {
		t.Error("expected Like to be called")
	}
}

func TestToggle_Unlike_WhenAlreadyLiked(t *testing.T) {
	authorID := uuid.New()
	userID := uuid.New()
	var unlikeCalled bool
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(authorID), nil
		},
	}
	lr := &mockLikeRepo{
		isLiked: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		unlike: func(_ context.Context, _, _ uuid.UUID) error {
			unlikeCalled = true
			return nil
		},
	}
	svc := newLikeService(lr, pr)

	liked, err := svc.Toggle(context.Background(), userID, uuid.New())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if liked {
		t.Error("expected liked=false after unlike")
	}
	if !unlikeCalled {
		t.Error("expected Unlike to be called")
	}
}

func TestToggle_PostNotFound(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}
	svc := newLikeService(&mockLikeRepo{}, pr)

	_, err := svc.Toggle(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != post.ErrPostNotFound {
		t.Errorf("expected ErrPostNotFound, got %v", err)
	}
}

func TestToggle_IsLikedError(t *testing.T) {
	pr := &mockPostRepo{
		getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
			return samplePost(uuid.New()), nil
		},
	}
	lr := &mockLikeRepo{
		isLiked: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	svc := newLikeService(lr, pr)

	_, err := svc.Toggle(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --------------------------------------------------------------------------
// CommentService — enrichment tests via GetComments (T010)
// --------------------------------------------------------------------------

func commentWithID(id uuid.UUID) *mockCommentRepo {
	return &mockCommentRepo{
		getByPost: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Comment, error) {
			return []*entity.Comment{{ID: id, AuthorID: uuid.New()}}, nil
		},
		countByPost: func(_ context.Context, _ uuid.UUID) (int64, error) { return 1, nil },
	}
}

func TestGetComments_EnrichLikes_WithViewer(t *testing.T) {
	postID := uuid.New()
	commentID := uuid.New()
	viewerID := uuid.New()
	cr := &mockCommentRepo{
		getByPost: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Comment, error) {
			return []*entity.Comment{{ID: commentID, AuthorID: uuid.New()}}, nil
		},
		countByPost: func(_ context.Context, _ uuid.UUID) (int64, error) { return 1, nil },
	}
	svc := newCommentService(cr, postExists(postID))
	svc.commentLikeRepo = &mockCommentLikeRepo{
		getLikedCommentIDs: func(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
			return ids, nil // all liked
		},
	}

	comments, _, err := svc.GetComments(context.Background(), postID, &viewerID, pagination.PaginationRequest{Limit: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if !comments[0].IsLiked {
		t.Error("expected IsLiked=true for returned comment")
	}
}

func TestGetComments_EnrichLikes_NilCommentLikeRepo(t *testing.T) {
	postID := uuid.New()
	commentID := uuid.New()
	viewerID := uuid.New()
	svc := newCommentService(commentWithID(commentID), postExists(postID))
	// commentLikeRepo is nil by default from newCommentService

	comments, _, err := svc.GetComments(context.Background(), postID, &viewerID, pagination.PaginationRequest{Limit: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) == 0 {
		t.Fatal("expected comments, got none")
	}
	if comments[0].IsLiked {
		t.Error("expected IsLiked=false when commentLikeRepo is nil")
	}
}

func TestGetComments_EnrichLikes_RepoError_NonFatal(t *testing.T) {
	postID := uuid.New()
	commentID := uuid.New()
	viewerID := uuid.New()
	cr := &mockCommentRepo{
		getByPost: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Comment, error) {
			return []*entity.Comment{{ID: commentID, AuthorID: uuid.New()}}, nil
		},
		countByPost: func(_ context.Context, _ uuid.UUID) (int64, error) { return 1, nil },
	}
	svc := newCommentService(cr, postExists(postID))
	svc.commentLikeRepo = &mockCommentLikeRepo{
		getLikedCommentIDs: func(_ context.Context, _ uuid.UUID, _ []uuid.UUID) ([]uuid.UUID, error) {
			return nil, pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}

	comments, _, err := svc.GetComments(context.Background(), postID, &viewerID, pagination.PaginationRequest{Limit: 20})
	if err != nil {
		t.Fatalf("enrichLikes error must be non-fatal, but got: %v", err)
	}
	if len(comments) != 1 {
		t.Errorf("expected 1 comment despite enrich error, got %d", len(comments))
	}
}

func TestGetComments_EnrichAuthors_Success(t *testing.T) {
	postID := uuid.New()
	authorID := uuid.New()
	commentID := uuid.New()
	cr := &mockCommentRepo{
		getByPost: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Comment, error) {
			return []*entity.Comment{{ID: commentID, AuthorID: authorID}}, nil
		},
		countByPost: func(_ context.Context, _ uuid.UUID) (int64, error) { return 1, nil },
	}
	svc := newCommentService(cr, postExists(postID))
	svc.userReader = &mockUserReader{
		getAuthorsByIDs: func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			m := make(map[uuid.UUID]*entity.Author, len(ids))
			for _, id := range ids {
				m[id] = &entity.Author{ID: id, Username: "tester"}
			}
			return m, nil
		},
	}

	comments, _, err := svc.GetComments(context.Background(), postID, nil, pagination.PaginationRequest{Limit: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Author == nil || comments[0].Author.Username != "tester" {
		t.Errorf("expected Author.Username=tester, got %v", comments[0].Author)
	}
}

func TestGetComments_EnrichAuthors_NilUserReader(t *testing.T) {
	postID := uuid.New()
	commentID := uuid.New()
	svc := newCommentService(commentWithID(commentID), postExists(postID))
	// userReader is nil by default from newCommentService

	comments, _, err := svc.GetComments(context.Background(), postID, nil, pagination.PaginationRequest{Limit: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) == 0 {
		t.Fatal("expected comments, got none")
	}
	if comments[0].Author != nil {
		t.Error("expected Author=nil when userReader is nil")
	}
}

func TestGetComments_EnrichAuthors_RepoError_NonFatal(t *testing.T) {
	postID := uuid.New()
	commentID := uuid.New()
	svc := newCommentService(commentWithID(commentID), postExists(postID))
	svc.userReader = &mockUserReader{
		getAuthorsByIDs: func(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return nil, pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}

	comments, _, err := svc.GetComments(context.Background(), postID, nil, pagination.PaginationRequest{Limit: 20})
	if err != nil {
		t.Fatalf("enrichAuthors error must be non-fatal, but got: %v", err)
	}
	if len(comments) != 1 {
		t.Errorf("expected 1 comment despite enrich error, got %d", len(comments))
	}
}

// --------------------------------------------------------------------------
// CommentService — persistCommentMentions tests (via CreateComment)
// --------------------------------------------------------------------------

func TestCreateComment_WithMentions_InsertsAndEnriches(t *testing.T) {
	postID := uuid.New()
	authorID := uuid.New()
	mentionedID := uuid.New()

	var insertedUserID uuid.UUID
	cmr := &mockCommentMentionRepo{
		insert: func(_ context.Context, _, uid uuid.UUID) error {
			insertedUserID = uid
			return nil
		},
	}
	svc := newCommentService(&mockCommentRepo{}, postExists(postID))
	svc.commentMentionRepo = cmr
	svc.userReader = &mockUserReader{
		getAuthorsByIDs: func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			m := make(map[uuid.UUID]*entity.Author, len(ids))
			for _, id := range ids {
				m[id] = &entity.Author{ID: id, Username: "mentioned"}
			}
			return m, nil
		},
	}

	c, err := svc.CreateComment(context.Background(), postID, authorID, nil, "hey @user", nil, []uuid.UUID{mentionedID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if insertedUserID != mentionedID {
		t.Errorf("expected Insert called with mentionedID, got %v", insertedUserID)
	}
	if len(c.Mentions) != 1 || c.Mentions[0].Username != "mentioned" {
		t.Errorf("expected 1 enriched mention, got %v", c.Mentions)
	}
}

func TestCreateComment_WithMentions_InsertError_NonFatal(t *testing.T) {
	postID := uuid.New()
	mentionedID := uuid.New()

	cmr := &mockCommentMentionRepo{
		insert: func(_ context.Context, _, _ uuid.UUID) error {
			return pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	svc := newCommentService(&mockCommentRepo{}, postExists(postID))
	svc.commentMentionRepo = cmr
	svc.userReader = &mockUserReader{}

	c, err := svc.CreateComment(context.Background(), postID, uuid.New(), nil, "hey @user", nil, []uuid.UUID{mentionedID})
	if err != nil {
		t.Fatalf("mention insert error must be non-fatal, got: %v", err)
	}
	if len(c.Mentions) != 0 {
		t.Errorf("expected 0 mentions after insert error, got %d", len(c.Mentions))
	}
}

// --------------------------------------------------------------------------
// CommentService — enrichCommentMentions tests (via GetComments)
// --------------------------------------------------------------------------

func TestGetComments_EnrichMentions_Success(t *testing.T) {
	postID := uuid.New()
	commentID := uuid.New()
	mentionedID := uuid.New()

	cr := &mockCommentRepo{
		getByPost: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Comment, error) {
			return []*entity.Comment{{ID: commentID, AuthorID: uuid.New()}}, nil
		},
		countByPost: func(_ context.Context, _ uuid.UUID) (int64, error) { return 1, nil },
	}
	svc := newCommentService(cr, postExists(postID))
	svc.commentMentionRepo = &mockCommentMentionRepo{
		getBatch: func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
			return map[uuid.UUID][]uuid.UUID{ids[0]: {mentionedID}}, nil
		},
	}
	svc.userReader = &mockUserReader{
		getAuthorsByIDs: func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			m := make(map[uuid.UUID]*entity.Author, len(ids))
			for _, id := range ids {
				m[id] = &entity.Author{ID: id, Username: "mentioned"}
			}
			return m, nil
		},
	}

	comments, _, err := svc.GetComments(context.Background(), postID, nil, pagination.PaginationRequest{Limit: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if len(comments[0].Mentions) != 1 || comments[0].Mentions[0].Username != "mentioned" {
		t.Errorf("expected 1 enriched mention, got %v", comments[0].Mentions)
	}
}

func TestGetComments_EnrichMentions_NilRepo(t *testing.T) {
	postID := uuid.New()
	commentID := uuid.New()
	svc := newCommentService(commentWithID(commentID), postExists(postID))
	// commentMentionRepo is nil — enrichCommentMentions should be a no-op

	comments, _, err := svc.GetComments(context.Background(), postID, nil, pagination.PaginationRequest{Limit: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if len(comments[0].Mentions) != 0 {
		t.Errorf("expected 0 mentions when repo is nil, got %d", len(comments[0].Mentions))
	}
}
