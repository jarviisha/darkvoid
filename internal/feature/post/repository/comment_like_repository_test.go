package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
)

// Like and Unlike carry two ids of the same type into a two-column query.
// Transposing them is a silent bug — the row lands on the wrong pair — so the
// assertions name each column rather than counting arguments.

func TestCommentLike_PassesUserAndCommentSeparately(t *testing.T) {
	userID, commentID := uuid.New(), uuid.New()
	var gotUser, gotComment uuid.UUID
	q := &fakeQuerier{likeComment: func(_ context.Context, arg db.LikeCommentParams) error {
		gotUser, gotComment = arg.UserID, arg.CommentID
		return nil
	}}
	r := &CommentLikeRepository{queries: q}

	if err := r.Like(context.Background(), userID, commentID); err != nil {
		t.Fatalf("Like: %v", err)
	}
	if gotUser != userID {
		t.Errorf("UserID: want %v, got %v", userID, gotUser)
	}
	if gotComment != commentID {
		t.Errorf("CommentID: want %v, got %v", commentID, gotComment)
	}
}

func TestCommentUnlike_PassesUserAndCommentSeparately(t *testing.T) {
	userID, commentID := uuid.New(), uuid.New()
	var gotUser, gotComment uuid.UUID
	q := &fakeQuerier{unlikeComment: func(_ context.Context, arg db.UnlikeCommentParams) error {
		gotUser, gotComment = arg.UserID, arg.CommentID
		return nil
	}}
	r := &CommentLikeRepository{queries: q}

	if err := r.Unlike(context.Background(), userID, commentID); err != nil {
		t.Fatalf("Unlike: %v", err)
	}
	if gotUser != userID {
		t.Errorf("UserID: want %v, got %v", userID, gotUser)
	}
	if gotComment != commentID {
		t.Errorf("CommentID: want %v, got %v", commentID, gotComment)
	}
}

func TestCommentLike_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{likeComment: func(context.Context, db.LikeCommentParams) error { return errBoom }}
	r := &CommentLikeRepository{queries: q}

	if err := r.Like(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("want an error from Like")
	}
}

// A failed lookup must not read as "not liked": the caller turns false into an
// unlike, so returning it alongside an error would make a database blip
// indistinguishable from a real state.
func TestCommentIsLiked_ErrorReturnsFalseAndError(t *testing.T) {
	q := &fakeQuerier{isCommentLiked: func(context.Context, db.IsCommentLikedParams) (bool, error) {
		return true, errBoom
	}}
	r := &CommentLikeRepository{queries: q}

	liked, err := r.IsLiked(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("want an error from IsLiked")
	}
	if liked {
		t.Error("want false alongside the error, got true")
	}
}

func TestCommentIsLiked_ReturnsQueryResult(t *testing.T) {
	userID, commentID := uuid.New(), uuid.New()
	var got db.IsCommentLikedParams
	q := &fakeQuerier{isCommentLiked: func(_ context.Context, arg db.IsCommentLikedParams) (bool, error) {
		got = arg
		return true, nil
	}}
	r := &CommentLikeRepository{queries: q}

	liked, err := r.IsLiked(context.Background(), userID, commentID)
	if err != nil {
		t.Fatalf("IsLiked: %v", err)
	}
	if !liked {
		t.Error("want true, got false")
	}
	if got.UserID != userID || got.CommentID != commentID {
		t.Errorf("params not passed through: %+v", got)
	}
}

// The viewer is one id and the candidate set is a separate array parameter.
// Passing the set where the viewer belongs would return another user's likes.
func TestGetLikedCommentIDs_PassesViewerAndCandidateSet(t *testing.T) {
	userID := uuid.New()
	liked, unliked := uuid.New(), uuid.New()
	var got db.GetLikedCommentIDsParams
	q := &fakeQuerier{likedCommentIDs: func(_ context.Context, arg db.GetLikedCommentIDsParams) ([]uuid.UUID, error) {
		got = arg
		return []uuid.UUID{liked}, nil
	}}
	r := &CommentLikeRepository{queries: q}

	ids, err := r.GetLikedCommentIDs(context.Background(), userID, []uuid.UUID{liked, unliked})
	if err != nil {
		t.Fatalf("GetLikedCommentIDs: %v", err)
	}
	if got.UserID != userID {
		t.Errorf("UserID: want %v, got %v", userID, got.UserID)
	}
	if len(got.Column2) != 2 || got.Column2[0] != liked || got.Column2[1] != unliked {
		t.Errorf("candidate set not passed through: %v", got.Column2)
	}
	if len(ids) != 1 || ids[0] != liked {
		t.Errorf("want [%v], got %v", liked, ids)
	}
}

func TestGetLikedCommentIDs_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{likedCommentIDs: func(context.Context, db.GetLikedCommentIDsParams) ([]uuid.UUID, error) {
		return nil, errBoom
	}}
	r := &CommentLikeRepository{queries: q}

	if _, err := r.GetLikedCommentIDs(context.Background(), uuid.New(), []uuid.UUID{uuid.New()}); err == nil {
		t.Fatal("want an error from GetLikedCommentIDs")
	}
}
