package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
)

// ---------------------------------------------------------------------------
// rowToPost — DeletedAt branching
// ---------------------------------------------------------------------------

func TestRowToPost_DeletedAt_NotSet(t *testing.T) {
	row := db.PostPost{
		ID:         uuid.New(),
		AuthorID:   uuid.New(),
		Content:    "hello",
		Visibility: "public",
	}
	p := rowToPost(row)
	if p.DeletedAt != nil {
		t.Errorf("expected DeletedAt nil when Valid=false, got %v", p.DeletedAt)
	}
}

func TestRowToPost_DeletedAt_Set(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Microsecond)
	row := db.PostPost{
		ID:         uuid.New(),
		Content:    "gone",
		Visibility: "public",
		DeletedAt:  pgtype.Timestamptz{Time: ts, Valid: true},
	}
	p := rowToPost(row)
	if p.DeletedAt == nil {
		t.Fatal("expected DeletedAt to be set when Valid=true")
	}
	if !p.DeletedAt.Equal(ts) {
		t.Errorf("expected DeletedAt=%v, got %v", ts, *p.DeletedAt)
	}
}

func TestRowToPost_FieldMapping(t *testing.T) {
	id := uuid.New()
	authorID := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	row := db.PostPost{
		ID:           id,
		AuthorID:     authorID,
		Content:      "content",
		Visibility:   "private",
		LikeCount:    7,
		CommentCount: 3,
		CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
	}
	p := rowToPost(row)
	if p.ID != id {
		t.Errorf("ID: want %v, got %v", id, p.ID)
	}
	if p.AuthorID != authorID {
		t.Errorf("AuthorID: want %v, got %v", authorID, p.AuthorID)
	}
	if p.Content != "content" {
		t.Errorf("Content: want content, got %v", p.Content)
	}
	if string(p.Visibility) != "private" {
		t.Errorf("Visibility: want private, got %v", p.Visibility)
	}
	if p.LikeCount != 7 || p.CommentCount != 3 {
		t.Errorf("counts: want 7/3, got %d/%d", p.LikeCount, p.CommentCount)
	}
	if !p.CreatedAt.Equal(now) || !p.UpdatedAt.Equal(now) {
		t.Errorf("timestamps not mapped correctly")
	}
}

// ---------------------------------------------------------------------------
// rowToComment — ParentID and DeletedAt branching
// ---------------------------------------------------------------------------

func TestRowToComment_NoParent_NoDeletedAt(t *testing.T) {
	row := db.PostComment{
		ID:       uuid.New(),
		PostID:   uuid.New(),
		AuthorID: uuid.New(),
		Content:  "comment",
	}
	c := rowToComment(row)
	if c.ParentID != nil {
		t.Errorf("expected ParentID nil, got %v", c.ParentID)
	}
	if c.DeletedAt != nil {
		t.Errorf("expected DeletedAt nil, got %v", c.DeletedAt)
	}
}

func TestRowToComment_WithParent(t *testing.T) {
	parentID := uuid.New()
	row := db.PostComment{
		ID:       uuid.New(),
		PostID:   uuid.New(),
		AuthorID: uuid.New(),
		Content:  "reply",
		ParentID: pgtype.UUID{Bytes: parentID, Valid: true},
	}
	c := rowToComment(row)
	if c.ParentID == nil {
		t.Fatal("expected ParentID to be set when Valid=true")
	}
	if *c.ParentID != parentID {
		t.Errorf("expected ParentID=%v, got %v", parentID, *c.ParentID)
	}
}

func TestRowToComment_WithDeletedAt(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Microsecond)
	row := db.PostComment{
		ID:        uuid.New(),
		PostID:    uuid.New(),
		AuthorID:  uuid.New(),
		Content:   "deleted",
		DeletedAt: pgtype.Timestamptz{Time: ts, Valid: true},
	}
	c := rowToComment(row)
	if c.DeletedAt == nil {
		t.Fatal("expected DeletedAt to be set when Valid=true")
	}
	if !c.DeletedAt.Equal(ts) {
		t.Errorf("expected DeletedAt=%v, got %v", ts, *c.DeletedAt)
	}
}

// ---------------------------------------------------------------------------
// followingCursorRowsToPosts — DeletedAt branching
// ---------------------------------------------------------------------------

func TestFollowingCursorRowsToPosts_DeletedAt(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Microsecond)
	rows := []db.GetFollowingPostsWithCursorRow{
		{ID: uuid.New(), DeletedAt: pgtype.Timestamptz{Valid: false}},
		{ID: uuid.New(), DeletedAt: pgtype.Timestamptz{Time: ts, Valid: true}},
	}
	posts := followingCursorRowsToPosts(rows)
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts, got %d", len(posts))
	}
	if posts[0].DeletedAt != nil {
		t.Errorf("post[0]: expected DeletedAt nil")
	}
	if posts[1].DeletedAt == nil {
		t.Fatal("post[1]: expected DeletedAt to be set")
	}
	if !posts[1].DeletedAt.Equal(ts) {
		t.Errorf("post[1]: expected DeletedAt=%v, got %v", ts, *posts[1].DeletedAt)
	}
}

// ---------------------------------------------------------------------------
// hashtagCursorRowsToPosts — DeletedAt branching
// ---------------------------------------------------------------------------

func TestHashtagCursorRowsToPosts_DeletedAt(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Microsecond)
	rows := []db.GetPostsByHashtagWithCursorRow{
		{ID: uuid.New(), DeletedAt: pgtype.Timestamptz{Valid: false}},
		{ID: uuid.New(), DeletedAt: pgtype.Timestamptz{Time: ts, Valid: true}},
	}
	posts := hashtagCursorRowsToPosts(rows)
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts, got %d", len(posts))
	}
	if posts[0].DeletedAt != nil {
		t.Errorf("post[0]: expected DeletedAt nil")
	}
	if posts[1].DeletedAt == nil {
		t.Fatal("post[1]: expected DeletedAt to be set")
	}
	if !posts[1].DeletedAt.Equal(ts) {
		t.Errorf("post[1]: expected DeletedAt=%v, got %v", ts, *posts[1].DeletedAt)
	}
}

// ---------------------------------------------------------------------------
// PostRepository.GetPostsByIDs — empty-slice guard (no DB call)
// ---------------------------------------------------------------------------

func TestGetPostsByIDs_NilSlice(t *testing.T) {
	r := NewPostRepository(nil)
	posts, err := r.GetPostsByIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if posts != nil {
		t.Errorf("expected nil posts, got %v", posts)
	}
}

func TestGetPostsByIDs_EmptySlice(t *testing.T) {
	r := NewPostRepository(nil)
	posts, err := r.GetPostsByIDs(context.Background(), []uuid.UUID{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if posts != nil {
		t.Errorf("expected nil posts, got %v", posts)
	}
}

// ---------------------------------------------------------------------------
// HashtagRepository.UpsertAndLink — empty-names guard (no DB call)
// ---------------------------------------------------------------------------

func TestUpsertAndLink_NilNames(t *testing.T) {
	r := NewHashtagRepository(nil)
	if err := r.UpsertAndLink(context.Background(), uuid.New(), nil); err != nil {
		t.Fatalf("expected nil error for nil names, got %v", err)
	}
}

func TestUpsertAndLink_EmptyNames(t *testing.T) {
	r := NewHashtagRepository(nil)
	if err := r.UpsertAndLink(context.Background(), uuid.New(), []string{}); err != nil {
		t.Fatalf("expected nil error for empty names, got %v", err)
	}
}
