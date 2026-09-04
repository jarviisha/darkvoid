package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
	apperrors "github.com/jarviisha/darkvoid/pkg/errors"
)

// A top-level comment and a reply differ only by whether ParentID is Valid.
// Sending a zero-but-Valid uuid would make every top-level comment a reply to
// nothing, so the nil case must arrive with Valid false.
func TestCommentCreate_TopLevelSendsInvalidParent(t *testing.T) {
	postID, authorID := uuid.New(), uuid.New()
	var got db.CreateCommentParams
	q := &mockQuerier{createComment: func(_ context.Context, arg db.CreateCommentParams) (db.PostComment, error) {
		got = arg
		return db.PostComment{ID: uuid.New(), PostID: arg.PostID, AuthorID: arg.AuthorID, Content: arg.Content}, nil
	}}
	r := &CommentRepository{queries: q}

	c, err := r.Create(context.Background(), postID, authorID, nil, "top level")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.PostID != postID || got.AuthorID != authorID || got.Content != "top level" {
		t.Errorf("params not passed through: %+v", got)
	}
	if got.ParentID.Valid {
		t.Errorf("a nil parent must be sent as an invalid uuid, got %v", got.ParentID)
	}
	if c.ParentID != nil {
		t.Errorf("ParentID: want nil, got %v", c.ParentID)
	}
}

func TestCommentCreate_ReplySendsValidParent(t *testing.T) {
	parentID := uuid.New()
	var got db.CreateCommentParams
	q := &mockQuerier{createComment: func(_ context.Context, arg db.CreateCommentParams) (db.PostComment, error) {
		got = arg
		return db.PostComment{ID: uuid.New(), Content: arg.Content, ParentID: arg.ParentID}, nil
	}}
	r := &CommentRepository{queries: q}

	c, err := r.Create(context.Background(), uuid.New(), uuid.New(), &parentID, "reply")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !got.ParentID.Valid || uuid.UUID(got.ParentID.Bytes) != parentID {
		t.Errorf("ParentID: want a valid %v, got %v", parentID, got.ParentID)
	}
	if c.ParentID == nil || *c.ParentID != parentID {
		t.Errorf("mapped ParentID: want %v, got %v", parentID, c.ParentID)
	}
}

func TestCommentCreate_ErrorIsMapped(t *testing.T) {
	q := &mockQuerier{createComment: func(context.Context, db.CreateCommentParams) (db.PostComment, error) {
		return db.PostComment{}, errBoom
	}}
	r := &CommentRepository{queries: q}

	if _, err := r.Create(context.Background(), uuid.New(), uuid.New(), nil, "x"); err == nil {
		t.Fatal("want an error from Create")
	}
}

func TestCommentGetByID_NoRowsBecomesNotFound(t *testing.T) {
	q := &mockQuerier{getCommentByID: func(context.Context, uuid.UUID) (db.PostComment, error) {
		return db.PostComment{}, pgx.ErrNoRows
	}}
	r := &CommentRepository{queries: q}

	_, err := r.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestCommentGetByID_MapsRow(t *testing.T) {
	id, postID, authorID := uuid.New(), uuid.New(), uuid.New()
	var got uuid.UUID
	q := &mockQuerier{getCommentByID: func(_ context.Context, arg uuid.UUID) (db.PostComment, error) {
		got = arg
		return db.PostComment{ID: id, PostID: postID, AuthorID: authorID, Content: "hi", LikeCount: 4}, nil
	}}
	r := &CommentRepository{queries: q}

	c, err := r.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != id {
		t.Errorf("id not passed through: want %v, got %v", id, got)
	}
	if c.ID != id || c.PostID != postID || c.AuthorID != authorID || c.Content != "hi" || c.LikeCount != 4 {
		t.Errorf("row not mapped: %+v", c)
	}
}

// Limit and offset are two int32s side by side; transposing them pages through
// the wrong window, which reads as missing comments rather than as an error.
func TestCommentGetByPost_PassesLimitAndOffsetSeparately(t *testing.T) {
	postID := uuid.New()
	var got db.GetCommentsByPostParams
	q := &mockQuerier{getCommentsByPost: func(_ context.Context, arg db.GetCommentsByPostParams) ([]db.PostComment, error) {
		got = arg
		return []db.PostComment{{ID: uuid.New(), Content: "a"}, {ID: uuid.New(), Content: "b"}}, nil
	}}
	r := &CommentRepository{queries: q}

	comments, err := r.GetByPost(context.Background(), postID, 20, 40)
	if err != nil {
		t.Fatalf("GetByPost: %v", err)
	}
	if got.PostID != postID {
		t.Errorf("PostID: want %v, got %v", postID, got.PostID)
	}
	if got.Limit != 20 {
		t.Errorf("Limit: want 20, got %d", got.Limit)
	}
	if got.Offset != 40 {
		t.Errorf("Offset: want 40, got %d", got.Offset)
	}
	if len(comments) != 2 || comments[1].Content != "b" {
		t.Errorf("rows not mapped in order: %+v", comments)
	}
}

func TestCommentGetByPost_ErrorIsMapped(t *testing.T) {
	q := &mockQuerier{getCommentsByPost: func(context.Context, db.GetCommentsByPostParams) ([]db.PostComment, error) {
		return nil, errBoom
	}}
	r := &CommentRepository{queries: q}

	if _, err := r.GetByPost(context.Background(), uuid.New(), 20, 0); err == nil {
		t.Fatal("want an error from GetByPost")
	}
}

// The parent arrives as a plain uuid and has to be wrapped Valid, or the query
// matches rows whose parent is null instead of the thread being read.
func TestCommentGetReplies_WrapsParentAsValid(t *testing.T) {
	parentID := uuid.New()
	var got db.GetRepliesParams
	q := &mockQuerier{getReplies: func(_ context.Context, arg db.GetRepliesParams) ([]db.PostComment, error) {
		got = arg
		return []db.PostComment{{ID: uuid.New(), Content: "reply"}}, nil
	}}
	r := &CommentRepository{queries: q}

	replies, err := r.GetReplies(context.Background(), parentID, 10, 5)
	if err != nil {
		t.Fatalf("GetReplies: %v", err)
	}
	if !got.ParentID.Valid {
		t.Fatal("ParentID must be sent as a valid uuid")
	}
	if uuid.UUID(got.ParentID.Bytes) != parentID {
		t.Errorf("ParentID: want %v, got %v", parentID, uuid.UUID(got.ParentID.Bytes))
	}
	if got.Limit != 10 || got.Offset != 5 {
		t.Errorf("paging: want 10/5, got %d/%d", got.Limit, got.Offset)
	}
	if len(replies) != 1 || replies[0].Content != "reply" {
		t.Errorf("rows not mapped: %+v", replies)
	}
}

func TestCommentGetReplies_ErrorIsMapped(t *testing.T) {
	q := &mockQuerier{getReplies: func(context.Context, db.GetRepliesParams) ([]db.PostComment, error) {
		return nil, errBoom
	}}
	r := &CommentRepository{queries: q}

	if _, err := r.GetReplies(context.Background(), uuid.New(), 10, 0); err == nil {
		t.Fatal("want an error from GetReplies")
	}
}

func TestCommentCountByPost_ReturnsCount(t *testing.T) {
	postID := uuid.New()
	var got uuid.UUID
	q := &mockQuerier{countCommentsByPost: func(_ context.Context, id uuid.UUID) (int64, error) {
		got = id
		return 17, nil
	}}
	r := &CommentRepository{queries: q}

	n, err := r.CountByPost(context.Background(), postID)
	if err != nil {
		t.Fatalf("CountByPost: %v", err)
	}
	if got != postID {
		t.Errorf("PostID: want %v, got %v", postID, got)
	}
	if n != 17 {
		t.Errorf("count: want 17, got %d", n)
	}
}

// A count that failed must not read as zero comments: the caller renders that
// as an empty thread rather than surfacing the failure.
func TestCommentCountByPost_ErrorReturnsZeroAndError(t *testing.T) {
	q := &mockQuerier{countCommentsByPost: func(context.Context, uuid.UUID) (int64, error) { return 9, errBoom }}
	r := &CommentRepository{queries: q}

	n, err := r.CountByPost(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("want an error from CountByPost")
	}
	if n != 0 {
		t.Errorf("want 0 alongside the error, got %d", n)
	}
}

func TestCommentDelete_PassesIDAndMapsError(t *testing.T) {
	id := uuid.New()
	var got uuid.UUID
	q := &mockQuerier{deleteComment: func(_ context.Context, arg uuid.UUID) error {
		got = arg
		return nil
	}}
	r := &CommentRepository{queries: q}

	if err := r.Delete(context.Background(), id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got != id {
		t.Errorf("id: want %v, got %v", id, got)
	}

	q.deleteComment = func(context.Context, uuid.UUID) error { return errBoom }
	if err := r.Delete(context.Background(), id); err == nil {
		t.Fatal("want an error from Delete")
	}
}

func TestCommentGetReplyCountsBatch_ErrorIsMapped(t *testing.T) {
	q := &mockQuerier{getReplyCountsBatch: func(context.Context, []uuid.UUID) ([]db.GetReplyCountsBatchRow, error) {
		return nil, errBoom
	}}
	r := &CommentRepository{queries: q}

	if _, err := r.GetReplyCountsBatch(context.Background(), []uuid.UUID{uuid.New()}); err == nil {
		t.Fatal("want an error from GetReplyCountsBatch")
	}
}

func TestCommentGetRepliesPreview_ErrorIsMapped(t *testing.T) {
	q := &mockQuerier{getRepliesPreview: func(context.Context, db.GetRepliesPreviewParams) ([]db.PostComment, error) {
		return nil, errBoom
	}}
	r := &CommentRepository{queries: q}

	if _, err := r.GetRepliesPreview(context.Background(), []uuid.UUID{uuid.New()}, 3); err == nil {
		t.Fatal("want an error from GetRepliesPreview")
	}
}
