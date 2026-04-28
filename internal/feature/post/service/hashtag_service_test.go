package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	post "github.com/jarviisha/darkvoid/internal/feature/post"
	postcache "github.com/jarviisha/darkvoid/internal/feature/post/cache"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	pkgerrors "github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// HashtagService — GetTrending tests
// --------------------------------------------------------------------------

func TestGetTrending_CacheHit(t *testing.T) {
	tags := []*entity.TrendingHashtag{{Name: "golang", Count: 10}}
	hc := &mockHashtagCache{
		getTrendingHashtags: func(_ context.Context) ([]*entity.TrendingHashtag, error) {
			return tags, nil
		},
	}
	hr := &mockHashtagRepo{
		getTrending: func(_ context.Context, _ int32) ([]*entity.TrendingHashtag, error) {
			t.Fatal("GetTrending repo should not be called on cache hit")
			return nil, nil
		},
	}
	svc := newHashtagService(hr, hc, &mockPostRepo{})

	got, err := svc.GetTrending(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 1 || got[0].Name != "golang" {
		t.Errorf("expected cached tags, got %v", got)
	}
}

func TestGetTrending_CacheMiss_RepoSuccess(t *testing.T) {
	tags := []*entity.TrendingHashtag{{Name: "go", Count: 5}}
	var setCalled bool
	hc := &mockHashtagCache{
		setTrendingHashtags: func(_ context.Context, _ []*entity.TrendingHashtag) error {
			setCalled = true
			return nil
		},
	}
	hr := &mockHashtagRepo{
		getTrending: func(_ context.Context, _ int32) ([]*entity.TrendingHashtag, error) {
			return tags, nil
		},
	}
	svc := newHashtagService(hr, hc, &mockPostRepo{})

	got, err := svc.GetTrending(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 1 || got[0].Name != "go" {
		t.Errorf("expected repo tags, got %v", got)
	}
	if !setCalled {
		t.Error("expected SetTrendingHashtags to be called")
	}
}

func TestGetTrending_CacheMiss_RepoError(t *testing.T) {
	hr := &mockHashtagRepo{
		getTrending: func(_ context.Context, _ int32) ([]*entity.TrendingHashtag, error) {
			return nil, pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	svc := newHashtagService(hr, &mockHashtagCache{}, &mockPostRepo{})

	_, err := svc.GetTrending(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetTrending_CacheSetError_NonFatal(t *testing.T) {
	tags := []*entity.TrendingHashtag{{Name: "rust", Count: 3}}
	hc := &mockHashtagCache{
		setTrendingHashtags: func(_ context.Context, _ []*entity.TrendingHashtag) error {
			return pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	hr := &mockHashtagRepo{
		getTrending: func(_ context.Context, _ int32) ([]*entity.TrendingHashtag, error) {
			return tags, nil
		},
	}
	svc := newHashtagService(hr, hc, &mockPostRepo{})

	got, err := svc.GetTrending(context.Background())
	if err != nil {
		t.Fatalf("expected no error despite cache set failure, got %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected tags to be returned, got %v", got)
	}
}

// --------------------------------------------------------------------------
// HashtagService — SearchHashtags tests
// --------------------------------------------------------------------------

func TestSearchHashtags_PrefixTooShort(t *testing.T) {
	svc := newHashtagService(&mockHashtagRepo{}, &mockHashtagCache{}, &mockPostRepo{})

	_, err := svc.SearchHashtags(context.Background(), "a")
	if err == nil {
		t.Fatal("expected error for too-short prefix")
	}
}

func TestSearchHashtags_CacheHit(t *testing.T) {
	hc := &mockHashtagCache{
		getSearchResults: func(_ context.Context, _ string) ([]string, error) {
			return []string{"golang", "gopher"}, nil
		},
	}
	hr := &mockHashtagRepo{
		searchByPrefix: func(_ context.Context, _ string, _ int32) ([]string, error) {
			t.Fatal("SearchByPrefix repo should not be called on cache hit")
			return nil, nil
		},
	}
	svc := newHashtagService(hr, hc, &mockPostRepo{})

	got, err := svc.SearchHashtags(context.Background(), "go")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 results, got %d", len(got))
	}
}

func TestSearchHashtags_CacheMiss_RepoSuccess(t *testing.T) {
	var setCalled bool
	hc := &mockHashtagCache{
		setSearchResults: func(_ context.Context, _ string, _ []string) error {
			setCalled = true
			return nil
		},
	}
	hr := &mockHashtagRepo{
		searchByPrefix: func(_ context.Context, _ string, _ int32) ([]string, error) {
			return []string{"rust"}, nil
		},
	}
	svc := newHashtagService(hr, hc, &mockPostRepo{})

	got, err := svc.SearchHashtags(context.Background(), "ru")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 1 || got[0] != "rust" {
		t.Errorf("expected [rust], got %v", got)
	}
	if !setCalled {
		t.Error("expected SetSearchResults to be called")
	}
}

func TestSearchHashtags_CacheMiss_RepoError(t *testing.T) {
	hr := &mockHashtagRepo{
		searchByPrefix: func(_ context.Context, _ string, _ int32) ([]string, error) {
			return nil, pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	svc := newHashtagService(hr, &mockHashtagCache{}, &mockPostRepo{})

	_, err := svc.SearchHashtags(context.Background(), "go")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSearchHashtags_CacheSetError_NonFatal(t *testing.T) {
	hc := &mockHashtagCache{
		setSearchResults: func(_ context.Context, _ string, _ []string) error {
			return pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	hr := &mockHashtagRepo{
		searchByPrefix: func(_ context.Context, _ string, _ int32) ([]string, error) {
			return []string{"python"}, nil
		},
	}
	svc := newHashtagService(hr, hc, &mockPostRepo{})

	got, err := svc.SearchHashtags(context.Background(), "py")
	if err != nil {
		t.Fatalf("expected no error despite cache set failure, got %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected results, got %v", got)
	}
}

// --------------------------------------------------------------------------
// HashtagService — GetPostsByHashtag tests
// --------------------------------------------------------------------------

func TestGetPostsByHashtag_CacheHit_Page1(t *testing.T) {
	postID := uuid.New()
	hc := &mockHashtagCache{
		getHashtagPostsPage1: func(_ context.Context, _ string) (*postcache.HashtagPostsPage, error) {
			return &postcache.HashtagPostsPage{Posts: []*entity.Post{{ID: postID}}}, nil
		},
	}
	hr := &mockHashtagRepo{
		getPostsByHashtag: func(_ context.Context, _ string, _ pgtype.Timestamptz, _ uuid.UUID, _ int32) ([]*entity.Post, error) {
			t.Fatal("GetPostsByHashtag repo should not be called on page 1 cache hit")
			return nil, nil
		},
	}
	svc := newHashtagService(hr, hc, &mockPostRepo{})

	got, _, err := svc.GetPostsByHashtag(context.Background(), "golang", nil, nil, 20)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 1 || got[0].ID != postID {
		t.Errorf("expected cached post, got %v", got)
	}
}

func TestGetPostsByHashtag_CacheMiss_RepoSuccess(t *testing.T) {
	postID := uuid.New()
	hr := &mockHashtagRepo{
		getPostsByHashtag: func(_ context.Context, _ string, _ pgtype.Timestamptz, _ uuid.UUID, _ int32) ([]*entity.Post, error) {
			return []*entity.Post{{ID: postID}}, nil
		},
	}
	svc := newHashtagService(hr, &mockHashtagCache{}, &mockPostRepo{})

	got, _, err := svc.GetPostsByHashtag(context.Background(), "golang", nil, nil, 20)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 1 || got[0].ID != postID {
		t.Errorf("expected repo post, got %v", got)
	}
}

func TestGetPostsByHashtag_Page2_SkipsCache(t *testing.T) {
	cursor := &post.UserPostCursor{CreatedAt: time.Now(), PostID: uuid.New().String()}
	var page1Called bool
	hc := &mockHashtagCache{
		getHashtagPostsPage1: func(_ context.Context, _ string) (*postcache.HashtagPostsPage, error) {
			page1Called = true
			return nil, errors.New("should not be called")
		},
	}
	svc := newHashtagService(&mockHashtagRepo{}, hc, &mockPostRepo{})

	svc.GetPostsByHashtag(context.Background(), "golang", nil, cursor, 20) //nolint:errcheck
	if page1Called {
		t.Error("GetHashtagPostsPage1 should not be called when cursor is non-nil (page 2+)")
	}
}

func TestGetPostsByHashtag_RepoError(t *testing.T) {
	hr := &mockHashtagRepo{
		getPostsByHashtag: func(_ context.Context, _ string, _ pgtype.Timestamptz, _ uuid.UUID, _ int32) ([]*entity.Post, error) {
			return nil, pkgerrors.NewInternalError(pkgerrors.ErrInternal)
		},
	}
	svc := newHashtagService(hr, &mockHashtagCache{}, &mockPostRepo{})

	_, _, err := svc.GetPostsByHashtag(context.Background(), "golang", nil, nil, 20)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetPostsByHashtag_DefaultLimit(t *testing.T) {
	var capturedLimit int32
	hr := &mockHashtagRepo{
		getPostsByHashtag: func(_ context.Context, _ string, _ pgtype.Timestamptz, _ uuid.UUID, limit int32) ([]*entity.Post, error) {
			capturedLimit = limit
			return nil, nil
		},
	}
	svc := newHashtagService(hr, &mockHashtagCache{}, &mockPostRepo{})

	svc.GetPostsByHashtag(context.Background(), "golang", nil, nil, 0) //nolint:errcheck
	// limit=0 triggers the defaulting logic → service uses limit=20, passes limit+1=21 to repo
	if capturedLimit != 21 {
		t.Errorf("expected repo called with limit 21 (20+1 for next-page detection), got %d", capturedLimit)
	}
}

// --------------------------------------------------------------------------
// HashtagService — enrichHashtagPosts tests (via GetPostsByHashtag)
// --------------------------------------------------------------------------

func TestGetPostsByHashtag_EnrichAuthors_Success(t *testing.T) {
	authorID := uuid.New()
	postID := uuid.New()
	hr := &mockHashtagRepo{
		getPostsByHashtag: func(_ context.Context, _ string, _ pgtype.Timestamptz, _ uuid.UUID, _ int32) ([]*entity.Post, error) {
			return []*entity.Post{{ID: postID, AuthorID: authorID}}, nil
		},
	}
	svc := newHashtagService(hr, &mockHashtagCache{}, &mockPostRepo{})
	svc.userReader = &mockUserReader{
		getAuthorsByIDs: func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			m := make(map[uuid.UUID]*entity.Author, len(ids))
			for _, id := range ids {
				m[id] = &entity.Author{ID: id, Username: "htagger"}
			}
			return m, nil
		},
	}

	posts, _, err := svc.GetPostsByHashtag(context.Background(), "golang", nil, nil, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	if posts[0].Author == nil || posts[0].Author.Username != "htagger" {
		t.Errorf("expected Author.Username=htagger, got %v", posts[0].Author)
	}
}

func TestGetPostsByHashtag_EnrichAuthors_RepoError_NonFatal(t *testing.T) {
	postID := uuid.New()
	hr := &mockHashtagRepo{
		getPostsByHashtag: func(_ context.Context, _ string, _ pgtype.Timestamptz, _ uuid.UUID, _ int32) ([]*entity.Post, error) {
			return []*entity.Post{{ID: postID, AuthorID: uuid.New()}}, nil
		},
	}
	svc := newHashtagService(hr, &mockHashtagCache{}, &mockPostRepo{})
	svc.userReader = &mockUserReader{
		getAuthorsByIDs: func(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return nil, errors.New("user service down")
		},
	}

	posts, _, err := svc.GetPostsByHashtag(context.Background(), "golang", nil, nil, 20)
	if err != nil {
		t.Fatalf("enrichAuthors error must be non-fatal, got: %v", err)
	}
	if len(posts) != 1 {
		t.Errorf("expected 1 post despite enrich error, got %d", len(posts))
	}
}
