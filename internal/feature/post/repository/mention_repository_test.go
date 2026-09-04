package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
)

// ---------------------------------------------------------------------------
// MentionRepository
// ---------------------------------------------------------------------------

func TestMentionInsert_PassesPostAndUserSeparately(t *testing.T) {
	postID, userID := uuid.New(), uuid.New()
	var got db.InsertMentionParams
	q := &fakeQuerier{insertMention: func(_ context.Context, arg db.InsertMentionParams) error {
		got = arg
		return nil
	}}
	r := &MentionRepository{queries: q}

	if err := r.Insert(context.Background(), postID, userID); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if got.PostID != postID {
		t.Errorf("PostID: want %v, got %v", postID, got.PostID)
	}
	if got.UserID != userID {
		t.Errorf("UserID: want %v, got %v", userID, got.UserID)
	}
}

func TestMentionInsert_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{insertMention: func(context.Context, db.InsertMentionParams) error { return errBoom }}
	r := &MentionRepository{queries: q}

	if err := r.Insert(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("want an error from Insert")
	}
}

func TestMentionDeleteByPost_PassesPostID(t *testing.T) {
	postID := uuid.New()
	var got uuid.UUID
	q := &fakeQuerier{delMentions: func(_ context.Context, id uuid.UUID) error {
		got = id
		return nil
	}}
	r := &MentionRepository{queries: q}

	if err := r.DeleteByPost(context.Background(), postID); err != nil {
		t.Fatalf("DeleteByPost: %v", err)
	}
	if got != postID {
		t.Errorf("PostID: want %v, got %v", postID, got)
	}
}

func TestMentionDeleteByPost_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{delMentions: func(context.Context, uuid.UUID) error { return errBoom }}
	r := &MentionRepository{queries: q}

	if err := r.DeleteByPost(context.Background(), uuid.New()); err == nil {
		t.Fatal("want an error from DeleteByPost")
	}
}

func TestMentionGetByPost_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{mentionsByPost: func(context.Context, uuid.UUID) ([]db.PostPostMention, error) {
		return nil, errBoom
	}}
	r := &MentionRepository{queries: q}

	if _, err := r.GetByPost(context.Background(), uuid.New()); err == nil {
		t.Fatal("want an error from GetByPost")
	}
}

// ---------------------------------------------------------------------------
// CommentMentionRepository
// ---------------------------------------------------------------------------

func TestCommentMentionInsert_PassesCommentAndUserSeparately(t *testing.T) {
	commentID, userID := uuid.New(), uuid.New()
	var got db.InsertCommentMentionParams
	q := &fakeQuerier{insertCmtMention: func(_ context.Context, arg db.InsertCommentMentionParams) error {
		got = arg
		return nil
	}}
	r := &CommentMentionRepository{queries: q}

	if err := r.Insert(context.Background(), commentID, userID); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if got.CommentID != commentID {
		t.Errorf("CommentID: want %v, got %v", commentID, got.CommentID)
	}
	if got.UserID != userID {
		t.Errorf("UserID: want %v, got %v", userID, got.UserID)
	}
}

func TestCommentMentionDeleteByComment_PassesCommentID(t *testing.T) {
	commentID := uuid.New()
	var got uuid.UUID
	q := &fakeQuerier{delCmtMentions: func(_ context.Context, id uuid.UUID) error {
		got = id
		return nil
	}}
	r := &CommentMentionRepository{queries: q}

	if err := r.DeleteByComment(context.Background(), commentID); err != nil {
		t.Fatalf("DeleteByComment: %v", err)
	}
	if got != commentID {
		t.Errorf("CommentID: want %v, got %v", commentID, got)
	}
}

func TestCommentMentionDeleteByComment_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{delCmtMentions: func(context.Context, uuid.UUID) error { return errBoom }}
	r := &CommentMentionRepository{queries: q}

	if err := r.DeleteByComment(context.Background(), uuid.New()); err == nil {
		t.Fatal("want an error from DeleteByComment")
	}
}

// GetByComment projects mention rows down to their user ids; the row carries a
// comment id too, and returning that instead would name the wrong people.
func TestCommentMentionGetByComment_ProjectsUserIDsInOrder(t *testing.T) {
	commentID := uuid.New()
	u1, u2 := uuid.New(), uuid.New()
	var got uuid.UUID
	q := &fakeQuerier{cmtMentionsByCmt: func(_ context.Context, id uuid.UUID) ([]db.PostCommentMention, error) {
		got = id
		return []db.PostCommentMention{
			{CommentID: commentID, UserID: u1},
			{CommentID: commentID, UserID: u2},
		}, nil
	}}
	r := &CommentMentionRepository{queries: q}

	ids, err := r.GetByComment(context.Background(), commentID)
	if err != nil {
		t.Fatalf("GetByComment: %v", err)
	}
	if got != commentID {
		t.Errorf("CommentID: want %v, got %v", commentID, got)
	}
	if len(ids) != 2 || ids[0] != u1 || ids[1] != u2 {
		t.Errorf("want [%v %v], got %v", u1, u2, ids)
	}
}

func TestCommentMentionGetByComment_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{cmtMentionsByCmt: func(context.Context, uuid.UUID) ([]db.PostCommentMention, error) {
		return nil, errBoom
	}}
	r := &CommentMentionRepository{queries: q}

	if _, err := r.GetByComment(context.Background(), uuid.New()); err == nil {
		t.Fatal("want an error from GetByComment")
	}
}

func TestCommentMentionGetBatch_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{cmtMentions: func(context.Context, []uuid.UUID) ([]db.PostCommentMention, error) {
		return nil, errBoom
	}}
	r := &CommentMentionRepository{queries: q}

	if _, err := r.GetBatch(context.Background(), []uuid.UUID{uuid.New()}); err == nil {
		t.Fatal("want an error from GetBatch")
	}
}
