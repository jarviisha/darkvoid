package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
)

// GetNamesByPostID projects hashtag rows down to their names; the row also
// carries an id, and returning that would render uuids as tags.
func TestHashtagGetNamesByPostID_ProjectsNamesInOrder(t *testing.T) {
	postID := uuid.New()
	var got uuid.UUID
	q := &fakeQuerier{hashtagsByPost: func(_ context.Context, id uuid.UUID) ([]db.PostHashtag, error) {
		got = id
		return []db.PostHashtag{
			{ID: uuid.New(), Name: "go"},
			{ID: uuid.New(), Name: "sqlc"},
		}, nil
	}}
	r := &HashtagRepository{queries: q}

	names, err := r.GetNamesByPostID(context.Background(), postID)
	if err != nil {
		t.Fatalf("GetNamesByPostID: %v", err)
	}
	if got != postID {
		t.Errorf("PostID: want %v, got %v", postID, got)
	}
	if len(names) != 2 || names[0] != "go" || names[1] != "sqlc" {
		t.Errorf("want [go sqlc], got %v", names)
	}
}

func TestHashtagGetNamesByPostID_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{hashtagsByPost: func(context.Context, uuid.UUID) ([]db.PostHashtag, error) {
		return nil, errBoom
	}}
	r := &HashtagRepository{queries: q}

	if _, err := r.GetNamesByPostID(context.Background(), uuid.New()); err == nil {
		t.Fatal("want an error from GetNamesByPostID")
	}
}

// The prefix travels in Column1 and the cap in Limit. Swapping them would send
// the prefix as a row cap, which the driver rejects rather than mis-answers —
// but the assertion names both so a silent reorder is caught too.
func TestHashtagSearchByPrefix_PassesPrefixAndLimit(t *testing.T) {
	var got db.SearchHashtagsByPrefixParams
	q := &fakeQuerier{tagsByPrefix: func(_ context.Context, arg db.SearchHashtagsByPrefixParams) ([]string, error) {
		got = arg
		return []string{"golang", "godoc"}, nil
	}}
	r := &HashtagRepository{queries: q}

	names, err := r.SearchByPrefix(context.Background(), "go", 5)
	if err != nil {
		t.Fatalf("SearchByPrefix: %v", err)
	}
	if got.Column1 != "go" {
		t.Errorf("prefix: want go, got %v", got.Column1)
	}
	if got.Limit != 5 {
		t.Errorf("Limit: want 5, got %d", got.Limit)
	}
	if len(names) != 2 || names[0] != "golang" {
		t.Errorf("results not returned: %v", names)
	}
}

func TestHashtagSearchByPrefix_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{tagsByPrefix: func(context.Context, db.SearchHashtagsByPrefixParams) ([]string, error) {
		return nil, errBoom
	}}
	r := &HashtagRepository{queries: q}

	if _, err := r.SearchByPrefix(context.Background(), "go", 5); err == nil {
		t.Fatal("want an error from SearchByPrefix")
	}
}

func TestHashtagGetNamesByPostIDs_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{hashtagsByIDs: func(context.Context, []uuid.UUID) ([]db.GetHashtagsByPostIDsRow, error) {
		return nil, errBoom
	}}
	r := &HashtagRepository{queries: q}

	if _, err := r.GetNamesByPostIDs(context.Background(), []uuid.UUID{uuid.New()}); err == nil {
		t.Fatal("want an error from GetNamesByPostIDs")
	}
}

func TestHashtagGetTrending_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{trendingTags: func(context.Context, int32) ([]db.GetTrendingHashtagsRow, error) {
		return nil, errBoom
	}}
	r := &HashtagRepository{queries: q}

	if _, err := r.GetTrending(context.Background(), 5); err == nil {
		t.Fatal("want an error from GetTrending")
	}
}

func TestHashtagGetPostsByHashtag_ErrorIsMapped(t *testing.T) {
	q := &fakeQuerier{postsByHashtag: func(context.Context, db.GetPostsByHashtagWithCursorParams) ([]db.GetPostsByHashtagWithCursorRow, error) {
		return nil, errBoom
	}}
	r := &HashtagRepository{queries: q}

	if _, err := r.GetPostsByHashtag(context.Background(), "go", pgtype.Timestamptz{}, uuid.New(), 20); err == nil {
		t.Fatal("want an error from GetPostsByHashtag")
	}
}
