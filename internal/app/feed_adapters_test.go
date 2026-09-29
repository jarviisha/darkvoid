package app

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
	postentity "github.com/jarviisha/darkvoid/internal/feature/post/entity"
	postservice "github.com/jarviisha/darkvoid/internal/feature/post/service"
	userentity "github.com/jarviisha/darkvoid/internal/feature/user/entity"
	userservice "github.com/jarviisha/darkvoid/internal/feature/user/service"
	pkgerrors "github.com/jarviisha/darkvoid/pkg/errors"
)

// These pin the feed context's view of posts: postReader is the one place that
// converts post and user entities into feed entities, so a field it drops here
// is a field every feed response silently loses.

type fakeFeedPostRepo struct {
	posts    []*postentity.Post
	err      error
	gotTS    pgtype.Timestamptz
	gotID    uuid.UUID
	gotLimit int32
}

func (r *fakeFeedPostRepo) record(ts pgtype.Timestamptz, id uuid.UUID, limit int32) ([]*postentity.Post, error) {
	r.gotTS, r.gotID, r.gotLimit = ts, id, limit
	return r.posts, r.err
}

func (r *fakeFeedPostRepo) GetFollowingPostsWithCursor(_ context.Context, _ []uuid.UUID, _ uuid.UUID, ts pgtype.Timestamptz, id uuid.UUID, limit int32) ([]*postentity.Post, error) {
	return r.record(ts, id, limit)
}

func (r *fakeFeedPostRepo) GetTrendingPosts(_ context.Context, limit int32) ([]*postentity.Post, error) {
	return r.record(pgtype.Timestamptz{}, uuid.Nil, limit)
}

func (r *fakeFeedPostRepo) GetPostsByIDs(context.Context, []uuid.UUID) ([]*postentity.Post, error) {
	return r.posts, r.err
}

func (r *fakeFeedPostRepo) GetDiscoverWithCursor(_ context.Context, ts pgtype.Timestamptz, id uuid.UUID, limit int32) ([]*postentity.Post, error) {
	return r.record(ts, id, limit)
}

type fakeFeedMediaRepo struct {
	media map[uuid.UUID][]*postentity.PostMedia
	err   error
}

func (r *fakeFeedMediaRepo) GetByPostsBatch(context.Context, []uuid.UUID) (map[uuid.UUID][]*postentity.PostMedia, error) {
	return r.media, r.err
}

type fakeFeedLikeRepo struct {
	liked []uuid.UUID
	err   error
	calls int
}

func (r *fakeFeedLikeRepo) GetLikedPostIDs(context.Context, uuid.UUID, []uuid.UUID) ([]uuid.UUID, error) {
	r.calls++
	return r.liked, r.err
}

type fakeFeedUserRepo struct {
	users []*userentity.User
	err   error
}

func (r *fakeFeedUserRepo) GetUsersByIDsAny(context.Context, []uuid.UUID) ([]*userentity.User, error) {
	return r.users, r.err
}

func (r *fakeFeedUserRepo) GetUsersByIDs(context.Context, []uuid.UUID) ([]*userentity.User, error) {
	panic("feed authors must include deactivated users")
}

type feedReaderFakes struct {
	posts *fakeFeedPostRepo
	media *fakeFeedMediaRepo
	likes *fakeFeedLikeRepo
	users *fakeFeedUserRepo
}

func newTestFeedPostReader(posts ...*postentity.Post) (*postReader, feedReaderFakes) {
	f := feedReaderFakes{
		posts: &fakeFeedPostRepo{posts: posts},
		media: &fakeFeedMediaRepo{},
		likes: &fakeFeedLikeRepo{},
		users: &fakeFeedUserRepo{},
	}
	return &postReader{
		postRepo: f.posts,
		hydrator: postservice.NewHydrator(postservice.HydratorDeps{
			Media: f.media,
			Likes: f.likes,
			Users: userservice.NewAuthorDirectory(f.users),
		}),
	}, f
}

func testFeedSourcePost() *postentity.Post {
	created := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	return &postentity.Post{
		ID:                uuid.New(),
		AuthorID:          uuid.New(),
		Content:           "hello",
		Visibility:        postentity.VisibilityFollowers,
		CreatedAt:         created,
		UpdatedAt:         created.Add(time.Minute),
		LikeCount:         7,
		CommentCount:      3,
		IsLiked:           true,
		IsFollowingAuthor: true,
	}
}

func TestToFeedPost_CopiesEveryField(t *testing.T) {
	p := testFeedSourcePost()
	p.Media = []*postentity.PostMedia{{
		ID: uuid.New(), PostID: p.ID, MediaKey: "k", MediaType: "image", Position: 2, CreatedAt: p.CreatedAt,
	}}
	p.Author = &postentity.Author{ID: p.AuthorID, Username: "a"}

	got := toFeedPost(p)
	if got.Author != p.Author {
		t.Fatalf("author = %+v, want %+v", got.Author, p.Author)
	}

	if got.ID != p.ID || got.AuthorID != p.AuthorID || got.Content != p.Content ||
		got.Visibility != "followers" || !got.CreatedAt.Equal(p.CreatedAt) || !got.UpdatedAt.Equal(p.UpdatedAt) ||
		got.LikeCount != 7 || got.CommentCount != 3 || !got.IsLiked || !got.IsFollowingAuthor {
		t.Fatalf("toFeedPost = %+v, want every scalar field of %+v", got, p)
	}
	want := feedentity.PostMedia{ID: p.Media[0].ID, PostID: p.ID, MediaKey: "k", MediaType: "image", Position: 2, CreatedAt: p.CreatedAt}
	if len(got.Media) != 1 || got.Media[0] != want {
		t.Fatalf("media = %+v, want [%+v]", got.Media, want)
	}
}

func TestFeedPostReader_GetPostsByIDs_FollowsRequestedOrderAndEnriches(t *testing.T) {
	a, b := testFeedSourcePost(), testFeedSourcePost()
	missing := uuid.New()
	avatar := "avatar-key"
	// The repository answers WHERE id = ANY in whatever order it likes.
	r, f := newTestFeedPostReader(a, b)
	f.media.media = map[uuid.UUID][]*postentity.PostMedia{b.ID: {{ID: uuid.New(), PostID: b.ID, MediaKey: "m"}}}
	f.users.users = []*userentity.User{{ID: b.AuthorID, Username: "bee", DisplayName: "Bee", AvatarKey: &avatar}}

	got, err := r.GetPostsByIDs(context.Background(), []uuid.UUID{b.ID, missing, a.ID})
	if err != nil {
		t.Fatalf("GetPostsByIDs: %v", err)
	}
	if len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Fatalf("order = %v, want [b a] with the missing id dropped", feedPostIDs(got))
	}
	if len(got[0].Media) != 1 || got[0].Media[0].MediaKey != "m" {
		t.Fatalf("media not attached: %+v", got[0].Media)
	}
	wantAuthor := feedentity.Author{ID: b.AuthorID, Username: "bee", DisplayName: "Bee", AvatarKey: &avatar}
	if got[0].Author == nil || *got[0].Author != wantAuthor {
		t.Fatalf("author = %+v, want %+v", got[0].Author, wantAuthor)
	}
	// An author the user repository does not return (deleted) stays nil rather
	// than failing the page.
	if got[1].Author != nil {
		t.Fatalf("unknown author = %+v, want nil", got[1].Author)
	}
}

// Media and authors are best-effort: a failure there serves the posts bare.
func TestFeedPostReader_EnrichmentFailuresAreNotFatal(t *testing.T) {
	p := testFeedSourcePost()
	r, f := newTestFeedPostReader(p)
	f.media.err = errors.New("media down")
	f.users.err = errors.New("users down")

	got, err := r.GetTrendingPosts(context.Background(), 5)
	if err != nil {
		t.Fatalf("GetTrendingPosts: %v", err)
	}
	if len(got) != 1 || got[0].ID != p.ID || got[0].Author != nil || len(got[0].Media) != 0 {
		t.Fatalf("got %+v, want the bare post", got)
	}
	if f.posts.gotLimit != 5 {
		t.Fatalf("limit = %d, want 5", f.posts.gotLimit)
	}
}

func TestFeedPostReader_CursorParams(t *testing.T) {
	cursorTime := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	cursorID := uuid.New()
	defaultTS, defaultID := feed.DefaultDiscoverPgParams()

	cases := []struct {
		name   string
		call   func(*postReader) error
		wantTS time.Time
		wantID uuid.UUID
	}{
		{"following, no cursor", func(r *postReader) error {
			_, err := r.GetFollowingPostsWithCursor(context.Background(), nil, uuid.New(), nil, 60)
			return err
		}, defaultTS.Time, defaultID},
		{"following, cursor", func(r *postReader) error {
			_, err := r.GetFollowingPostsWithCursor(context.Background(), nil, uuid.New(), &feed.FollowingCursor{CreatedAt: cursorTime, PostID: cursorID.String()}, 60)
			return err
		}, cursorTime, cursorID},
		{"discover, no cursor", func(r *postReader) error {
			_, err := r.GetDiscoverWithCursor(context.Background(), nil, 60, nil)
			return err
		}, defaultTS.Time, defaultID},
		{"discover, cursor", func(r *postReader) error {
			_, err := r.GetDiscoverWithCursor(context.Background(), &feed.DiscoverCursor{CreatedAt: cursorTime, PostID: cursorID.String()}, 60, nil)
			return err
		}, cursorTime, cursorID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, f := newTestFeedPostReader()
			if err := tc.call(r); err != nil {
				t.Fatalf("call: %v", err)
			}
			if !f.posts.gotTS.Valid || !f.posts.gotTS.Time.Equal(tc.wantTS) || f.posts.gotID != tc.wantID || f.posts.gotLimit != 60 {
				t.Fatalf("params = (%v, %v, %d), want (%v, %v, 60)", f.posts.gotTS.Time, f.posts.gotID, f.posts.gotLimit, tc.wantTS, tc.wantID)
			}
		})
	}
}

func TestFeedPostReader_ErrorStatuses(t *testing.T) {
	bad := "not-a-uuid"
	cases := []struct {
		name    string
		repoErr error
		call    func(*postReader) error
		want    int
	}{
		{"following bad cursor id", nil, func(r *postReader) error {
			_, err := r.GetFollowingPostsWithCursor(context.Background(), nil, uuid.New(), &feed.FollowingCursor{PostID: bad}, 1)
			return err
		}, http.StatusBadRequest},
		{"discover bad cursor id", nil, func(r *postReader) error {
			_, err := r.GetDiscoverWithCursor(context.Background(), &feed.DiscoverCursor{PostID: bad}, 1, nil)
			return err
		}, http.StatusBadRequest},
		{"following repo error", errors.New("db down"), func(r *postReader) error {
			_, err := r.GetFollowingPostsWithCursor(context.Background(), nil, uuid.New(), nil, 1)
			return err
		}, http.StatusInternalServerError},
		{"trending repo error", errors.New("db down"), func(r *postReader) error {
			_, err := r.GetTrendingPosts(context.Background(), 1)
			return err
		}, http.StatusInternalServerError},
		{"by ids repo error", errors.New("db down"), func(r *postReader) error {
			_, err := r.GetPostsByIDs(context.Background(), []uuid.UUID{uuid.New()})
			return err
		}, http.StatusInternalServerError},
		{"discover repo error", errors.New("db down"), func(r *postReader) error {
			_, err := r.GetDiscoverWithCursor(context.Background(), nil, 1, nil)
			return err
		}, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, f := newTestFeedPostReader()
			f.posts.err = tc.repoErr
			appErr := pkgerrors.GetAppError(tc.call(r))
			if appErr == nil || appErr.HTTPStatus != tc.want {
				t.Fatalf("error = %v, want HTTP %d", appErr, tc.want)
			}
		})
	}
}

// Discover marks the viewer's likes; an anonymous viewer costs no like lookup,
// and a failed lookup serves the page unliked.
func TestFeedPostReader_DiscoverLikedState(t *testing.T) {
	liked, unliked := testFeedSourcePost(), testFeedSourcePost()
	liked.IsLiked, unliked.IsLiked = false, false

	r, f := newTestFeedPostReader(liked, unliked)
	f.likes.liked = []uuid.UUID{liked.ID}
	viewer := uuid.New()
	got, err := r.GetDiscoverWithCursor(context.Background(), nil, 10, &viewer)
	if err != nil {
		t.Fatalf("GetDiscoverWithCursor: %v", err)
	}
	if !got[0].IsLiked || got[1].IsLiked {
		t.Fatalf("liked = [%v %v], want [true false]", got[0].IsLiked, got[1].IsLiked)
	}

	r, f = newTestFeedPostReader(testFeedSourcePost())
	if _, err := r.GetDiscoverWithCursor(context.Background(), nil, 10, nil); err != nil {
		t.Fatalf("anonymous GetDiscoverWithCursor: %v", err)
	}
	if f.likes.calls != 0 {
		t.Fatalf("like lookups for an anonymous viewer = %d, want 0", f.likes.calls)
	}

	p := testFeedSourcePost()
	p.IsLiked = false
	r, f = newTestFeedPostReader(p)
	f.likes.err = errors.New("likes down")
	got, err = r.GetDiscoverWithCursor(context.Background(), nil, 10, &viewer)
	if err != nil || len(got) != 1 || got[0].IsLiked {
		t.Fatalf("got %+v, %v; want the post served unliked", got, err)
	}
}

func feedPostIDs(posts []*feedentity.Post) []uuid.UUID {
	ids := make([]uuid.UUID, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}
	return ids
}
