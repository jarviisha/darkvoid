package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
)

func TestPostLike_PassesUserAndPostSeparately(t *testing.T) {
	userID, postID := uuid.New(), uuid.New()
	var got db.LikePostParams
	q := &mockQuerier{likePost: func(_ context.Context, arg db.LikePostParams) error {
		got = arg
		return nil
	}}
	r := &LikeRepository{queries: q}

	if err := r.Like(context.Background(), userID, postID); err != nil {
		t.Fatalf("Like: %v", err)
	}
	if got.UserID != userID {
		t.Errorf("UserID: want %v, got %v", userID, got.UserID)
	}
	if got.PostID != postID {
		t.Errorf("PostID: want %v, got %v", postID, got.PostID)
	}
}

func TestPostLike_ErrorIsMapped(t *testing.T) {
	q := &mockQuerier{likePost: func(context.Context, db.LikePostParams) error { return errBoom }}
	r := &LikeRepository{queries: q}

	if err := r.Like(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("want an error from Like")
	}
}

func TestPostUnlike_PassesUserAndPostSeparately(t *testing.T) {
	userID, postID := uuid.New(), uuid.New()
	var got db.UnlikePostParams
	q := &mockQuerier{unlikePost: func(_ context.Context, arg db.UnlikePostParams) error {
		got = arg
		return nil
	}}
	r := &LikeRepository{queries: q}

	if err := r.Unlike(context.Background(), userID, postID); err != nil {
		t.Fatalf("Unlike: %v", err)
	}
	if got.UserID != userID {
		t.Errorf("UserID: want %v, got %v", userID, got.UserID)
	}
	if got.PostID != postID {
		t.Errorf("PostID: want %v, got %v", postID, got.PostID)
	}
}

func TestPostUnlike_ErrorIsMapped(t *testing.T) {
	q := &mockQuerier{unlikePost: func(context.Context, db.UnlikePostParams) error { return errBoom }}
	r := &LikeRepository{queries: q}

	if err := r.Unlike(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("want an error from Unlike")
	}
}

func TestPostIsLiked_ReturnsQueryResult(t *testing.T) {
	userID, postID := uuid.New(), uuid.New()
	var got db.IsLikedParams
	q := &mockQuerier{isLiked: func(_ context.Context, arg db.IsLikedParams) (bool, error) {
		got = arg
		return true, nil
	}}
	r := &LikeRepository{queries: q}

	liked, err := r.IsLiked(context.Background(), userID, postID)
	if err != nil {
		t.Fatalf("IsLiked: %v", err)
	}
	if !liked {
		t.Error("want true, got false")
	}
	if got.UserID != userID || got.PostID != postID {
		t.Errorf("params not passed through: %+v", got)
	}
}

// Same contract as the comment side: a failed lookup must not read as
// "not liked", or a database blip turns into an unlike.
func TestPostIsLiked_ErrorReturnsFalseAndError(t *testing.T) {
	q := &mockQuerier{isLiked: func(context.Context, db.IsLikedParams) (bool, error) {
		return true, errBoom
	}}
	r := &LikeRepository{queries: q}

	liked, err := r.IsLiked(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("want an error from IsLiked")
	}
	if liked {
		t.Error("want false alongside the error, got true")
	}
}

func TestPostLikeCount_ReturnsCount(t *testing.T) {
	postID := uuid.New()
	var got uuid.UUID
	q := &mockQuerier{countLikes: func(_ context.Context, id uuid.UUID) (int64, error) {
		got = id
		return 42, nil
	}}
	r := &LikeRepository{queries: q}

	n, err := r.Count(context.Background(), postID)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if got != postID {
		t.Errorf("PostID: want %v, got %v", postID, got)
	}
	if n != 42 {
		t.Errorf("count: want 42, got %d", n)
	}
}
