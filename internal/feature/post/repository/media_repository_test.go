package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
)

// ---------------------------------------------------------------------------
// MediaRepository
// ---------------------------------------------------------------------------

func TestMediaAdd_PassesAttachmentAndMapsRow(t *testing.T) {
	postID := uuid.New()
	created := time.Now().UTC().Truncate(time.Microsecond)
	var got db.AddPostMediaParams
	q := &fakeQuerier{addPostMedia: func(_ context.Context, arg db.AddPostMediaParams) (db.PostPostMedium, error) {
		got = arg
		return db.PostPostMedium{
			ID: uuid.New(), PostID: arg.PostID, MediaKey: arg.MediaKey,
			MediaType: arg.MediaType, Position: arg.Position, CreatedAt: ts(created),
		}, nil
	}}
	r := &MediaRepository{queries: q}

	m, err := r.Add(context.Background(), postID, "uploads/a.png", "image", 2)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got.PostID != postID || got.MediaKey != "uploads/a.png" || got.MediaType != "image" || got.Position != 2 {
		t.Errorf("params not passed through: %+v", got)
	}
	if m.MediaKey != "uploads/a.png" || m.MediaType != "image" || m.Position != 2 {
		t.Errorf("row not mapped: %+v", m)
	}
	if !m.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt: want %v, got %v", created, m.CreatedAt)
	}
}

func TestMediaAdd_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{addPostMedia: func(context.Context, db.AddPostMediaParams) (db.PostPostMedium, error) {
		return db.PostPostMedium{}, errBoom
	}}
	r := &MediaRepository{queries: q}

	if _, err := r.Add(context.Background(), uuid.New(), "k", "image", 0); err == nil {
		t.Fatal("want an error from Add")
	}
}

// Delete is scoped by post as well as by id, so a caller cannot detach media
// from someone else's post by guessing an id. Both must reach the query.
func TestMediaDelete_ScopesByPostAsWellAsID(t *testing.T) {
	mediaID, postID := uuid.New(), uuid.New()
	var got db.DeletePostMediaParams
	q := &fakeQuerier{delPostMedia: func(_ context.Context, arg db.DeletePostMediaParams) error {
		got = arg
		return nil
	}}
	r := &MediaRepository{queries: q}

	if err := r.Delete(context.Background(), mediaID, postID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got.ID != mediaID {
		t.Errorf("ID: want %v, got %v", mediaID, got.ID)
	}
	if got.PostID != postID {
		t.Errorf("PostID: want %v, got %v", postID, got.PostID)
	}
}

func TestMediaDelete_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{delPostMedia: func(context.Context, db.DeletePostMediaParams) error { return errBoom }}
	r := &MediaRepository{queries: q}

	if err := r.Delete(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("want an error from Delete")
	}
}

func TestMediaDeleteAllByPost_PassesPostID(t *testing.T) {
	postID := uuid.New()
	var got uuid.UUID
	q := &fakeQuerier{delAllPostMedia: func(_ context.Context, id uuid.UUID) error {
		got = id
		return nil
	}}
	r := &MediaRepository{queries: q}

	if err := r.DeleteAllByPost(context.Background(), postID); err != nil {
		t.Fatalf("DeleteAllByPost: %v", err)
	}
	if got != postID {
		t.Errorf("PostID: want %v, got %v", postID, got)
	}
}

func TestMediaDeleteAllByPost_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{delAllPostMedia: func(context.Context, uuid.UUID) error { return errBoom }}
	r := &MediaRepository{queries: q}

	if err := r.DeleteAllByPost(context.Background(), uuid.New()); err == nil {
		t.Fatal("want an error from DeleteAllByPost")
	}
}

func TestMediaGetByPost_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{postMedia: func(context.Context, uuid.UUID) ([]db.PostPostMedium, error) {
		return nil, errBoom
	}}
	r := &MediaRepository{queries: q}

	if _, err := r.GetByPost(context.Background(), uuid.New()); err == nil {
		t.Fatal("want an error from GetByPost")
	}
}

// ---------------------------------------------------------------------------
// CommentMediaRepository
// ---------------------------------------------------------------------------

func TestCommentMediaAdd_PassesAttachmentAndMapsRow(t *testing.T) {
	commentID := uuid.New()
	created := time.Now().UTC().Truncate(time.Microsecond)
	var got db.AddCommentMediaParams
	q := &fakeQuerier{addCmtMedia: func(_ context.Context, arg db.AddCommentMediaParams) (db.PostCommentMedium, error) {
		got = arg
		return db.PostCommentMedium{
			ID: uuid.New(), CommentID: arg.CommentID, MediaKey: arg.MediaKey,
			MediaType: arg.MediaType, Position: arg.Position, CreatedAt: ts(created),
		}, nil
	}}
	r := &CommentMediaRepository{queries: q}

	m, err := r.Add(context.Background(), commentID, "uploads/b.mp4", "video", 1)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got.CommentID != commentID || got.MediaKey != "uploads/b.mp4" || got.MediaType != "video" || got.Position != 1 {
		t.Errorf("params not passed through: %+v", got)
	}
	if m.CommentID != commentID || m.MediaType != "video" || m.Position != 1 {
		t.Errorf("row not mapped: %+v", m)
	}
	if !m.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt: want %v, got %v", created, m.CreatedAt)
	}
}

func TestCommentMediaAdd_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{addCmtMedia: func(context.Context, db.AddCommentMediaParams) (db.PostCommentMedium, error) {
		return db.PostCommentMedium{}, errBoom
	}}
	r := &CommentMediaRepository{queries: q}

	if _, err := r.Add(context.Background(), uuid.New(), "k", "image", 0); err == nil {
		t.Fatal("want an error from Add")
	}
}

func TestCommentMediaGetByComment_MapsRowsInOrder(t *testing.T) {
	commentID := uuid.New()
	var got uuid.UUID
	q := &fakeQuerier{cmtMedia: func(_ context.Context, id uuid.UUID) ([]db.PostCommentMedium, error) {
		got = id
		return []db.PostCommentMedium{
			{ID: uuid.New(), CommentID: commentID, MediaKey: "k0", MediaType: "image", Position: 0},
			{ID: uuid.New(), CommentID: commentID, MediaKey: "k1", MediaType: "video", Position: 1},
		}, nil
	}}
	r := &CommentMediaRepository{queries: q}

	media, err := r.GetByComment(context.Background(), commentID)
	if err != nil {
		t.Fatalf("GetByComment: %v", err)
	}
	if got != commentID {
		t.Errorf("CommentID: want %v, got %v", commentID, got)
	}
	if len(media) != 2 || media[0].MediaKey != "k0" || media[1].MediaKey != "k1" || media[1].Position != 1 {
		t.Errorf("rows not mapped in order: %+v", media)
	}
}

func TestCommentMediaGetByComment_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{cmtMedia: func(context.Context, uuid.UUID) ([]db.PostCommentMedium, error) {
		return nil, errBoom
	}}
	r := &CommentMediaRepository{queries: q}

	if _, err := r.GetByComment(context.Background(), uuid.New()); err == nil {
		t.Fatal("want an error from GetByComment")
	}
}

func TestCommentMediaDeleteAllByComment_PassesCommentID(t *testing.T) {
	commentID := uuid.New()
	var got uuid.UUID
	q := &fakeQuerier{delAllCmtMedia: func(_ context.Context, id uuid.UUID) error {
		got = id
		return nil
	}}
	r := &CommentMediaRepository{queries: q}

	if err := r.DeleteAllByComment(context.Background(), commentID); err != nil {
		t.Fatalf("DeleteAllByComment: %v", err)
	}
	if got != commentID {
		t.Errorf("CommentID: want %v, got %v", commentID, got)
	}
}

func TestCommentMediaDeleteAllByComment_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{delAllCmtMedia: func(context.Context, uuid.UUID) error { return errBoom }}
	r := &CommentMediaRepository{queries: q}

	if err := r.DeleteAllByComment(context.Background(), uuid.New()); err == nil {
		t.Fatal("want an error from DeleteAllByComment")
	}
}

func TestCommentMediaGetByCommentsBatch_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{cmtMediaBatch: func(context.Context, []uuid.UUID) ([]db.PostCommentMedium, error) {
		return nil, errBoom
	}}
	r := &CommentMediaRepository{queries: q}

	if _, err := r.GetByCommentsBatch(context.Background(), []uuid.UUID{uuid.New()}); err == nil {
		t.Fatal("want an error from GetByCommentsBatch")
	}
}
