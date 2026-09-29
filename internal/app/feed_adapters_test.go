package app

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	postentity "github.com/jarviisha/darkvoid/internal/feature/post/entity"
	postservice "github.com/jarviisha/darkvoid/internal/feature/post/service"
)

type fakeFeedPostRepo struct{ posts []*postentity.Post }

func (r *fakeFeedPostRepo) GetFollowingPostsWithCursor(context.Context, []uuid.UUID, uuid.UUID, pgtype.Timestamptz, uuid.UUID, int32) ([]*postentity.Post, error) {
	return r.posts, nil
}
func (r *fakeFeedPostRepo) GetTrendingPosts(context.Context, int32) ([]*postentity.Post, error) {
	return r.posts, nil
}
func (r *fakeFeedPostRepo) GetPostsByIDs(context.Context, []uuid.UUID) ([]*postentity.Post, error) {
	return r.posts, nil
}
func (r *fakeFeedPostRepo) GetDiscoverWithCursor(context.Context, pgtype.Timestamptz, uuid.UUID, int32) ([]*postentity.Post, error) {
	return r.posts, nil
}

type fakeFeedMedia struct{}

func (fakeFeedMedia) GetByPostsBatch(_ context.Context, ids []uuid.UUID) (map[uuid.UUID][]*postentity.PostMedia, error) {
	m := make(map[uuid.UUID][]*postentity.PostMedia, len(ids))
	for _, id := range ids {
		m[id] = []*postentity.PostMedia{{PostID: id, MediaKey: "k"}}
	}
	return m, nil
}

type fakeFeedLikes struct{ calls int }

func (l *fakeFeedLikes) GetLikedPostIDs(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	l.calls++
	return ids, nil
}

type fakeFeedAuthors struct{}

func (fakeFeedAuthors) GetAuthorsByIDs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*postentity.Author, error) {
	m := make(map[uuid.UUID]*postentity.Author, len(ids))
	for _, id := range ids {
		m[id] = &postentity.Author{ID: id, Username: "u"}
	}
	return m, nil
}

func newTestPostReader(posts []*postentity.Post, likes *fakeFeedLikes) *postReader {
	return &postReader{
		postRepo: &fakeFeedPostRepo{posts: posts},
		hydrator: postservice.NewHydrator(postservice.HydratorDeps{
			Media: fakeFeedMedia{},
			Likes: likes,
			Users: fakeFeedAuthors{},
		}),
	}
}

func TestPostReader_Discover_FillsMediaAuthorAndLiked(t *testing.T) {
	likes := &fakeFeedLikes{}
	p := &postentity.Post{ID: uuid.New(), AuthorID: uuid.New()}
	viewer := uuid.New()

	got, err := newTestPostReader([]*postentity.Post{p}, likes).GetDiscoverWithCursor(context.Background(), nil, 20, &viewer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Author == nil || got[0].Author.ID != p.AuthorID || len(got[0].Media) != 1 || !got[0].IsLiked {
		t.Errorf("got %+v, want author, media and liked filled", got)
	}
}

// Trending is cached for every viewer, so it must never carry one viewer's
// liked flags.
func TestPostReader_Trending_NoViewerFields(t *testing.T) {
	likes := &fakeFeedLikes{}
	p := &postentity.Post{ID: uuid.New(), AuthorID: uuid.New()}

	got, err := newTestPostReader([]*postentity.Post{p}, likes).GetTrendingPosts(context.Background(), 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if likes.calls != 0 {
		t.Errorf("likes queried %d times for trending", likes.calls)
	}
	if len(got) != 1 || got[0].Author == nil || len(got[0].Media) != 1 {
		t.Errorf("got %+v, want author and media filled", got)
	}
}

func TestPostReader_GetPostsByIDs_KeepsRequestedOrder(t *testing.T) {
	a := &postentity.Post{ID: uuid.New(), AuthorID: uuid.New()}
	b := &postentity.Post{ID: uuid.New(), AuthorID: uuid.New()}

	got, err := newTestPostReader([]*postentity.Post{a, b}, &fakeFeedLikes{}).GetPostsByIDs(context.Background(), []uuid.UUID{b.ID, uuid.New(), a.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Errorf("got %v, want [b a]", got)
	}
}
