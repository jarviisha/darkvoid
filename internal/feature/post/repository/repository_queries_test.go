package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	apperrors "github.com/jarviisha/darkvoid/pkg/errors"
)

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

var errBoom = errors.New("boom")

// ---------------------------------------------------------------------------
// PostRepository
// ---------------------------------------------------------------------------

func TestPostCreate_MapsParamsAndRow(t *testing.T) {
	authorID := uuid.New()
	var got db.CreatePostParams
	q := &mockQuerier{createPost: func(_ context.Context, arg db.CreatePostParams) (db.PostPost, error) {
		got = arg
		return db.PostPost{ID: uuid.New(), AuthorID: arg.AuthorID, Content: arg.Content, Visibility: arg.Visibility}, nil
	}}
	r := &PostRepository{queries: q}

	p, err := r.Create(context.Background(), authorID, "hello", entity.VisibilityFollowers)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.AuthorID != authorID || got.Content != "hello" || got.Visibility != string(entity.VisibilityFollowers) {
		t.Errorf("params not passed through: %+v", got)
	}
	if p.Visibility != entity.VisibilityFollowers {
		t.Errorf("Visibility: want %v, got %v", entity.VisibilityFollowers, p.Visibility)
	}
}

func TestPostGetByID_NoRowsBecomesNotFound(t *testing.T) {
	q := &mockQuerier{getPostByID: func(context.Context, uuid.UUID) (db.PostPost, error) {
		return db.PostPost{}, pgx.ErrNoRows
	}}
	r := &PostRepository{queries: q}

	_, err := r.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPostUpdate_UniqueViolationBecomesConflict(t *testing.T) {
	q := &mockQuerier{updatePost: func(context.Context, db.UpdatePostParams) (db.PostPost, error) {
		return db.PostPost{}, &pgconn.PgError{Code: "23505", ConstraintName: "posts_pkey"}
	}}
	r := &PostRepository{queries: q}

	_, err := r.Update(context.Background(), uuid.New(), "x", entity.VisibilityPublic)
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("want *apperrors.AppError, got %T (%v)", err, err)
	}
	if appErr.Code != "CONFLICT" {
		t.Errorf("Code: want CONFLICT, got %s", appErr.Code)
	}
	if appErr.Details["constraint"] != "posts_pkey" {
		t.Errorf("constraint detail: want posts_pkey, got %v", appErr.Details["constraint"])
	}
}

func TestPostDelete_PropagatesError(t *testing.T) {
	q := &mockQuerier{deletePost: func(context.Context, uuid.UUID) error { return errBoom }}
	r := &PostRepository{queries: q}

	if err := r.Delete(context.Background(), uuid.New()); err == nil {
		t.Fatal("want an error from Delete")
	}
}

func TestGetFollowingPostsWithCursor_PassesCursorAndMapsRows(t *testing.T) {
	authorIDs := []uuid.UUID{uuid.New(), uuid.New()}
	viewer := uuid.New()
	cursorTS := ts(time.Now().UTC().Truncate(time.Microsecond))
	cursorID := uuid.New()
	deleted := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	var got db.GetFollowingPostsWithCursorParams
	q := &mockQuerier{getFollowingPostsWithCursor: func(_ context.Context, arg db.GetFollowingPostsWithCursorParams) ([]db.GetFollowingPostsWithCursorRow, error) {
		got = arg
		return []db.GetFollowingPostsWithCursorRow{
			{ID: uuid.New(), AuthorID: authorIDs[0], Content: "live", Visibility: "public", LikeCount: 4},
			{ID: uuid.New(), AuthorID: authorIDs[1], Content: "gone", Visibility: "public", DeletedAt: ts(deleted)},
		}, nil
	}}
	r := &PostRepository{queries: q}

	posts, err := r.GetFollowingPostsWithCursor(context.Background(), authorIDs, viewer, cursorTS, cursorID, 60)
	if err != nil {
		t.Fatalf("GetFollowingPostsWithCursor: %v", err)
	}
	if len(got.Column1) != 2 || got.Column2 != viewer || got.Column3 != cursorTS || got.Column4 != cursorID || got.Limit != 60 {
		t.Errorf("cursor params not passed through: %+v", got)
	}
	if len(posts) != 2 {
		t.Fatalf("want 2 posts, got %d", len(posts))
	}
	if posts[0].DeletedAt != nil {
		t.Error("post[0] should not be marked deleted")
	}
	if posts[1].DeletedAt == nil || !posts[1].DeletedAt.Equal(deleted) {
		t.Errorf("post[1] DeletedAt: want %v, got %v", deleted, posts[1].DeletedAt)
	}
}

func TestGetByAuthorWithCursor_PassesVisibilityFilters(t *testing.T) {
	authorID := uuid.New()
	var got db.GetUserPostsWithCursorParams
	q := &mockQuerier{getUserPostsWithCursor: func(_ context.Context, arg db.GetUserPostsWithCursorParams) ([]db.PostPost, error) {
		got = arg
		return []db.PostPost{{ID: uuid.New(), Visibility: "private"}}, nil
	}}
	r := &PostRepository{queries: q}

	posts, err := r.GetByAuthorWithCursor(context.Background(), authorID, pgtype.Timestamptz{}, uuid.Nil, []string{"public", "private"}, 20)
	if err != nil {
		t.Fatalf("GetByAuthorWithCursor: %v", err)
	}
	if got.AuthorID != authorID || len(got.Column4) != 2 || got.Column4[1] != "private" || got.Limit != 20 {
		t.Errorf("params not passed through: %+v", got)
	}
	if len(posts) != 1 || posts[0].Visibility != entity.VisibilityPrivate {
		t.Errorf("row not mapped: %+v", posts)
	}
}

func TestGetDiscoverWithCursor_MapsRows(t *testing.T) {
	cursorTS := ts(time.Now().UTC())
	cursorID := uuid.New()
	var got db.GetDiscoverWithCursorParams
	q := &mockQuerier{getDiscoverWithCursor: func(_ context.Context, arg db.GetDiscoverWithCursorParams) ([]db.PostPost, error) {
		got = arg
		return []db.PostPost{{ID: uuid.New(), Content: "a"}, {ID: uuid.New(), Content: "b"}}, nil
	}}
	r := &PostRepository{queries: q}

	posts, err := r.GetDiscoverWithCursor(context.Background(), cursorTS, cursorID, 20)
	if err != nil {
		t.Fatalf("GetDiscoverWithCursor: %v", err)
	}
	if got.CursorCreatedAt != cursorTS || got.CursorPostID != cursorID || got.Limit != 20 {
		t.Errorf("params not passed through: %+v", got)
	}
	if len(posts) != 2 || posts[1].Content != "b" {
		t.Errorf("rows not mapped in order: %+v", posts)
	}
}

func TestGetTrendingPosts_PropagatesError(t *testing.T) {
	q := &mockQuerier{getTrendingPosts: func(context.Context, int32) ([]db.PostPost, error) {
		return nil, errBoom
	}}
	r := &PostRepository{queries: q}

	if _, err := r.GetTrendingPosts(context.Background(), 10); err == nil {
		t.Fatal("want an error from GetTrendingPosts")
	}
}

// ---------------------------------------------------------------------------
// PostRepository.GetPostsByIDs — the hand-written scan, previously DB-only
// ---------------------------------------------------------------------------

func TestGetPostsByIDs_ScansRowsAndMapsDeletedAt(t *testing.T) {
	id1, id2 := uuid.New(), uuid.New()
	author := uuid.New()
	created := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	deleted := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	row := func(id uuid.UUID, vis string, del pgtype.Timestamptz) func(...any) error {
		return func(dest ...any) error {
			*dest[0].(*uuid.UUID) = id
			*dest[1].(*uuid.UUID) = author
			*dest[2].(*string) = "body"
			*dest[3].(*string) = vis
			*dest[4].(*int64) = 5
			*dest[5].(*int64) = 2
			*dest[6].(*pgtype.Timestamptz) = ts(created)
			*dest[7].(*pgtype.Timestamptz) = ts(created)
			*dest[8].(*pgtype.Timestamptz) = del
			return nil
		}
	}
	rows := &mockRows{scans: []func(...any) error{
		row(id1, "public", pgtype.Timestamptz{}),
		row(id2, "private", ts(deleted)),
	}}
	dbtx := &mockDBTX{query: func(context.Context, string, ...interface{}) (pgx.Rows, error) { return rows, nil }}
	r := &PostRepository{dbtx: dbtx}

	posts, err := r.GetPostsByIDs(context.Background(), []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("GetPostsByIDs: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("want 2 posts, got %d", len(posts))
	}
	if posts[0].ID != id1 || posts[0].AuthorID != author || posts[0].Content != "body" {
		t.Errorf("post[0] not scanned: %+v", posts[0])
	}
	if posts[0].Visibility != entity.VisibilityPublic || posts[1].Visibility != entity.VisibilityPrivate {
		t.Errorf("visibility not mapped: %v / %v", posts[0].Visibility, posts[1].Visibility)
	}
	if !posts[0].CreatedAt.Equal(created) || !posts[0].UpdatedAt.Equal(created) {
		t.Error("timestamps not unwrapped from pgtype")
	}
	if posts[0].DeletedAt != nil {
		t.Error("post[0] should not be marked deleted")
	}
	if posts[1].DeletedAt == nil || !posts[1].DeletedAt.Equal(deleted) {
		t.Errorf("post[1] DeletedAt: want %v, got %v", deleted, posts[1].DeletedAt)
	}
	if !rows.closed {
		t.Error("rows must be closed")
	}
}

func TestGetPostsByIDs_QueryErrorIsMapped(t *testing.T) {
	dbtx := &mockDBTX{query: func(context.Context, string, ...interface{}) (pgx.Rows, error) {
		return nil, errBoom
	}}
	r := &PostRepository{dbtx: dbtx}

	if _, err := r.GetPostsByIDs(context.Background(), []uuid.UUID{uuid.New()}); err == nil {
		t.Fatal("want an error when the query fails")
	}
}

func TestGetPostsByIDs_ScanErrorIsMapped(t *testing.T) {
	rows := &mockRows{scans: []func(...any) error{func(...any) error { return errBoom }}}
	dbtx := &mockDBTX{query: func(context.Context, string, ...interface{}) (pgx.Rows, error) { return rows, nil }}
	r := &PostRepository{dbtx: dbtx}

	if _, err := r.GetPostsByIDs(context.Background(), []uuid.UUID{uuid.New()}); err == nil {
		t.Fatal("want an error when a row fails to scan")
	}
}

// A result set that ends early reports the failure through Err, not through Next.
// Returning the partial page as a success would silently truncate a feed hydration.
func TestGetPostsByIDs_RowsErrIsReported(t *testing.T) {
	rows := &mockRows{err: errBoom}
	dbtx := &mockDBTX{query: func(context.Context, string, ...interface{}) (pgx.Rows, error) { return rows, nil }}
	r := &PostRepository{dbtx: dbtx}

	if _, err := r.GetPostsByIDs(context.Background(), []uuid.UUID{uuid.New()}); err == nil {
		t.Fatal("want an error when rows.Err() is set")
	}
}

// ---------------------------------------------------------------------------
// MentionRepository / CommentMentionRepository — grouping
// ---------------------------------------------------------------------------

func TestMentionGetBatch_GroupsByPost(t *testing.T) {
	postA, postB := uuid.New(), uuid.New()
	u1, u2, u3 := uuid.New(), uuid.New(), uuid.New()
	q := &mockQuerier{getMentionsBatch: func(context.Context, []uuid.UUID) ([]db.PostPostMention, error) {
		return []db.PostPostMention{
			{PostID: postA, UserID: u1},
			{PostID: postB, UserID: u2},
			{PostID: postA, UserID: u3},
		}, nil
	}}
	r := &MentionRepository{queries: q}

	got, err := r.GetBatch(context.Background(), []uuid.UUID{postA, postB})
	if err != nil {
		t.Fatalf("GetBatch: %v", err)
	}
	if len(got[postA]) != 2 || got[postA][0] != u1 || got[postA][1] != u3 {
		t.Errorf("postA mentions: want [%v %v], got %v", u1, u3, got[postA])
	}
	if len(got[postB]) != 1 || got[postB][0] != u2 {
		t.Errorf("postB mentions: want [%v], got %v", u2, got[postB])
	}
}

func TestMentionGetByPost_ReturnsUserIDs(t *testing.T) {
	u1, u2 := uuid.New(), uuid.New()
	q := &mockQuerier{getMentionsByPost: func(context.Context, uuid.UUID) ([]db.PostPostMention, error) {
		return []db.PostPostMention{{UserID: u1}, {UserID: u2}}, nil
	}}
	r := &MentionRepository{queries: q}

	ids, err := r.GetByPost(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetByPost: %v", err)
	}
	if len(ids) != 2 || ids[0] != u1 || ids[1] != u2 {
		t.Errorf("want [%v %v], got %v", u1, u2, ids)
	}
}

func TestCommentMentionGetBatch_GroupsByComment(t *testing.T) {
	cmtA, cmtB := uuid.New(), uuid.New()
	u1, u2 := uuid.New(), uuid.New()
	q := &mockQuerier{getCommentMentionsBatch: func(context.Context, []uuid.UUID) ([]db.PostCommentMention, error) {
		return []db.PostCommentMention{
			{CommentID: cmtA, UserID: u1},
			{CommentID: cmtB, UserID: u2},
		}, nil
	}}
	r := &CommentMentionRepository{queries: q}

	got, err := r.GetBatch(context.Background(), []uuid.UUID{cmtA, cmtB})
	if err != nil {
		t.Fatalf("GetBatch: %v", err)
	}
	if len(got) != 2 || got[cmtA][0] != u1 || got[cmtB][0] != u2 {
		t.Errorf("grouping wrong: %v", got)
	}
}

// ---------------------------------------------------------------------------
// MediaRepository / CommentMediaRepository
// ---------------------------------------------------------------------------

func TestMediaGetByPost_MapsRows(t *testing.T) {
	postID := uuid.New()
	created := time.Now().UTC().Truncate(time.Microsecond)
	q := &mockQuerier{getPostMedia: func(context.Context, uuid.UUID) ([]db.PostPostMedium, error) {
		return []db.PostPostMedium{
			{ID: uuid.New(), PostID: postID, MediaKey: "k0", MediaType: "image", Position: 0, CreatedAt: ts(created)},
			{ID: uuid.New(), PostID: postID, MediaKey: "k1", MediaType: "video", Position: 1, CreatedAt: ts(created)},
		}, nil
	}}
	r := &MediaRepository{queries: q}

	media, err := r.GetByPost(context.Background(), postID)
	if err != nil {
		t.Fatalf("GetByPost: %v", err)
	}
	if len(media) != 2 || media[0].MediaKey != "k0" || media[1].MediaType != "video" || media[1].Position != 1 {
		t.Errorf("rows not mapped: %+v", media)
	}
	if !media[0].CreatedAt.Equal(created) {
		t.Errorf("CreatedAt: want %v, got %v", created, media[0].CreatedAt)
	}
}

// The empty guard must not reach the database: a `= ANY('{}')` round trip is
// pure cost, and the caller expects an empty map rather than nil.
func TestMediaGetByPostsBatch_EmptyIDsSkipsQuery(t *testing.T) {
	dbtx := &mockDBTX{query: func(context.Context, string, ...interface{}) (pgx.Rows, error) {
		t.Fatal("must not query for an empty id set")
		return nil, nil
	}}
	r := &MediaRepository{dbtx: dbtx}

	got, err := r.GetByPostsBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetByPostsBatch: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("want an empty non-nil map, got %v", got)
	}
}

func TestMediaGetByPostsBatch_GroupsByPost(t *testing.T) {
	postA, postB := uuid.New(), uuid.New()
	created := time.Now().UTC().Truncate(time.Microsecond)

	row := func(post uuid.UUID, key string, pos int32) func(...any) error {
		return func(dest ...any) error {
			*dest[0].(*uuid.UUID) = uuid.New()
			*dest[1].(*uuid.UUID) = post
			*dest[2].(*string) = key
			*dest[3].(*string) = "image"
			*dest[4].(*int32) = pos
			*dest[5].(*pgtype.Timestamptz) = ts(created)
			return nil
		}
	}
	rows := &mockRows{scans: []func(...any) error{
		row(postA, "a0", 0), row(postA, "a1", 1), row(postB, "b0", 0),
	}}
	dbtx := &mockDBTX{query: func(context.Context, string, ...interface{}) (pgx.Rows, error) { return rows, nil }}
	r := &MediaRepository{dbtx: dbtx}

	got, err := r.GetByPostsBatch(context.Background(), []uuid.UUID{postA, postB})
	if err != nil {
		t.Fatalf("GetByPostsBatch: %v", err)
	}
	if len(got[postA]) != 2 || got[postA][0].MediaKey != "a0" || got[postA][1].MediaKey != "a1" {
		t.Errorf("postA media: %+v", got[postA])
	}
	if len(got[postB]) != 1 || got[postB][0].MediaKey != "b0" {
		t.Errorf("postB media: %+v", got[postB])
	}
	if !rows.closed {
		t.Error("rows must be closed")
	}
}

func TestCommentMediaGetByCommentsBatch_GroupsByComment(t *testing.T) {
	cmtA, cmtB := uuid.New(), uuid.New()
	q := &mockQuerier{getCommentMediaBatch: func(context.Context, []uuid.UUID) ([]db.PostCommentMedium, error) {
		return []db.PostCommentMedium{
			{ID: uuid.New(), CommentID: cmtA, MediaKey: "a0", Position: 0},
			{ID: uuid.New(), CommentID: cmtB, MediaKey: "b0", Position: 0},
			{ID: uuid.New(), CommentID: cmtA, MediaKey: "a1", Position: 1},
		}, nil
	}}
	r := &CommentMediaRepository{queries: q}

	got, err := r.GetByCommentsBatch(context.Background(), []uuid.UUID{cmtA, cmtB})
	if err != nil {
		t.Fatalf("GetByCommentsBatch: %v", err)
	}
	if len(got[cmtA]) != 2 || got[cmtA][1].MediaKey != "a1" {
		t.Errorf("cmtA media: %+v", got[cmtA])
	}
	if len(got[cmtB]) != 1 {
		t.Errorf("cmtB media: %+v", got[cmtB])
	}
}

// ---------------------------------------------------------------------------
// HashtagRepository
// ---------------------------------------------------------------------------

func TestHashtagGetNamesByPostIDs_GroupsByPost(t *testing.T) {
	postA, postB := uuid.New(), uuid.New()
	q := &mockQuerier{getHashtagsByPostIDs: func(context.Context, []uuid.UUID) ([]db.GetHashtagsByPostIDsRow, error) {
		return []db.GetHashtagsByPostIDsRow{
			{PostID: postA, Name: "go"},
			{PostID: postB, Name: "rust"},
			{PostID: postA, Name: "sql"},
		}, nil
	}}
	r := &HashtagRepository{queries: q}

	got, err := r.GetNamesByPostIDs(context.Background(), []uuid.UUID{postA, postB})
	if err != nil {
		t.Fatalf("GetNamesByPostIDs: %v", err)
	}
	if len(got[postA]) != 2 || got[postA][0] != "go" || got[postA][1] != "sql" {
		t.Errorf("postA tags: %v", got[postA])
	}
	if len(got[postB]) != 1 || got[postB][0] != "rust" {
		t.Errorf("postB tags: %v", got[postB])
	}
}

func TestHashtagGetTrending_MapsRows(t *testing.T) {
	q := &mockQuerier{getTrendingHashtags: func(_ context.Context, limit int32) ([]db.GetTrendingHashtagsRow, error) {
		if limit != 5 {
			t.Errorf("limit: want 5, got %d", limit)
		}
		return []db.GetTrendingHashtagsRow{{Name: "go", Count: 12}, {Name: "sql", Count: 3}}, nil
	}}
	r := &HashtagRepository{queries: q}

	got, err := r.GetTrending(context.Background(), 5)
	if err != nil {
		t.Fatalf("GetTrending: %v", err)
	}
	if len(got) != 2 || got[0].Name != "go" || got[0].Count != 12 || got[1].Count != 3 {
		t.Errorf("rows not mapped: %+v", got)
	}
}

func TestHashtagGetPostsByHashtag_PassesCursorAndMapsRows(t *testing.T) {
	cursorTS := ts(time.Now().UTC())
	cursorID := uuid.New()
	deleted := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	var got db.GetPostsByHashtagWithCursorParams
	q := &mockQuerier{getPostsByHashtagWithCursor: func(_ context.Context, arg db.GetPostsByHashtagWithCursorParams) ([]db.GetPostsByHashtagWithCursorRow, error) {
		got = arg
		return []db.GetPostsByHashtagWithCursorRow{
			{ID: uuid.New(), Content: "live", Visibility: "public"},
			{ID: uuid.New(), Content: "gone", Visibility: "public", DeletedAt: ts(deleted)},
		}, nil
	}}
	r := &HashtagRepository{queries: q}

	posts, err := r.GetPostsByHashtag(context.Background(), "go", cursorTS, cursorID, 20)
	if err != nil {
		t.Fatalf("GetPostsByHashtag: %v", err)
	}
	if got.Name != "go" || got.Column2 != cursorTS || got.Column3 != cursorID || got.Limit != 20 {
		t.Errorf("params not passed through: %+v", got)
	}
	if len(posts) != 2 || posts[0].DeletedAt != nil || posts[1].DeletedAt == nil {
		t.Errorf("rows not mapped: %+v", posts)
	}
}

// ---------------------------------------------------------------------------
// CommentRepository
// ---------------------------------------------------------------------------

func TestCommentGetReplyCountsBatch_KeysByRootID(t *testing.T) {
	rootA, rootB := uuid.New(), uuid.New()
	q := &mockQuerier{getReplyCountsBatch: func(context.Context, []uuid.UUID) ([]db.GetReplyCountsBatchRow, error) {
		return []db.GetReplyCountsBatchRow{
			{RootID: pgtype.UUID{Bytes: rootA, Valid: true}, Count: 3},
			{RootID: pgtype.UUID{Bytes: rootB, Valid: true}, Count: 0},
		}, nil
	}}
	r := &CommentRepository{queries: q}

	got, err := r.GetReplyCountsBatch(context.Background(), []uuid.UUID{rootA, rootB})
	if err != nil {
		t.Fatalf("GetReplyCountsBatch: %v", err)
	}
	if got[rootA] != 3 || got[rootB] != 0 {
		t.Errorf("counts: want 3/0, got %d/%d", got[rootA], got[rootB])
	}
}

// A preview row with no parent belongs to no bucket. Keying it under uuid.Nil
// would attach a top-level comment to every caller that looked up the zero id.
func TestCommentGetRepliesPreview_SkipsRowsWithNoParent(t *testing.T) {
	parent := uuid.New()
	q := &mockQuerier{getRepliesPreview: func(_ context.Context, arg db.GetRepliesPreviewParams) ([]db.PostComment, error) {
		if arg.Column2 != 3 {
			t.Errorf("limitPerParent: want 3, got %d", arg.Column2)
		}
		return []db.PostComment{
			{ID: uuid.New(), Content: "orphan"},
			{ID: uuid.New(), Content: "reply", ParentID: pgtype.UUID{Bytes: parent, Valid: true}},
		}, nil
	}}
	r := &CommentRepository{queries: q}

	got, err := r.GetRepliesPreview(context.Background(), []uuid.UUID{parent}, 3)
	if err != nil {
		t.Fatalf("GetRepliesPreview: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want one bucket, got %d: %v", len(got), got)
	}
	if len(got[parent]) != 1 || got[parent][0].Content != "reply" {
		t.Errorf("parent bucket: %+v", got[parent])
	}
	if _, ok := got[uuid.Nil]; ok {
		t.Error("a parentless row must not be bucketed under the zero uuid")
	}
}

// ---------------------------------------------------------------------------
// LikeRepository
// ---------------------------------------------------------------------------

func TestLikeGetLikedPostIDs_PassesViewerAndIDs(t *testing.T) {
	userID := uuid.New()
	liked := uuid.New()
	var got db.GetLikedPostIDsParams
	q := &mockQuerier{getLikedPostIDs: func(_ context.Context, arg db.GetLikedPostIDsParams) ([]uuid.UUID, error) {
		got = arg
		return []uuid.UUID{liked}, nil
	}}
	r := &LikeRepository{queries: q}

	ids, err := r.GetLikedPostIDs(context.Background(), userID, []uuid.UUID{liked, uuid.New()})
	if err != nil {
		t.Fatalf("GetLikedPostIDs: %v", err)
	}
	if got.UserID != userID || len(got.Column2) != 2 {
		t.Errorf("params not passed through: %+v", got)
	}
	if len(ids) != 1 || ids[0] != liked {
		t.Errorf("want [%v], got %v", liked, ids)
	}
}

func TestLikeCount_PropagatesError(t *testing.T) {
	q := &mockQuerier{countLikes: func(context.Context, uuid.UUID) (int64, error) { return 0, errBoom }}
	r := &LikeRepository{queries: q}

	n, err := r.Count(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("want an error from Count")
	}
	if n != 0 {
		t.Errorf("want 0 on error, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// PostSearchRepository
// ---------------------------------------------------------------------------

func TestSearchByQuery_PassesPagingAndMapsRows(t *testing.T) {
	created := time.Now().UTC().Truncate(time.Microsecond)
	deleted := created.Add(time.Minute)
	var got db.SearchPostsParams
	q := &mockQuerier{searchPosts: func(_ context.Context, arg db.SearchPostsParams) ([]db.SearchPostsRow, error) {
		got = arg
		return []db.SearchPostsRow{
			{ID: uuid.New(), Content: "hit", Visibility: "public", LikeCount: 9, CommentCount: 1, CreatedAt: ts(created), UpdatedAt: ts(created)},
			{ID: uuid.New(), Content: "gone", Visibility: "public", DeletedAt: ts(deleted)},
		}, nil
	}}
	r := &PostSearchRepository{queries: q}

	posts, err := r.SearchByQuery(context.Background(), "golang", 10, 20)
	if err != nil {
		t.Fatalf("SearchByQuery: %v", err)
	}
	if got.Query != "golang" || got.Limit != 10 || got.Offset != 20 {
		t.Errorf("params not passed through: %+v", got)
	}
	if len(posts) != 2 {
		t.Fatalf("want 2 posts, got %d", len(posts))
	}
	if posts[0].Content != "hit" || posts[0].LikeCount != 9 || posts[0].CommentCount != 1 {
		t.Errorf("post[0] not mapped: %+v", posts[0])
	}
	if !posts[0].CreatedAt.Equal(created) {
		t.Errorf("CreatedAt: want %v, got %v", created, posts[0].CreatedAt)
	}
	if posts[0].DeletedAt != nil {
		t.Error("post[0] should not be marked deleted")
	}
	if posts[1].DeletedAt == nil || !posts[1].DeletedAt.Equal(deleted) {
		t.Errorf("post[1] DeletedAt: want %v, got %v", deleted, posts[1].DeletedAt)
	}
}

func TestSearchByQuery_PropagatesError(t *testing.T) {
	q := &mockQuerier{searchPosts: func(context.Context, db.SearchPostsParams) ([]db.SearchPostsRow, error) {
		return nil, errBoom
	}}
	r := &PostSearchRepository{queries: q}

	if _, err := r.SearchByQuery(context.Background(), "x", 10, 0); err == nil {
		t.Fatal("want an error from SearchByQuery")
	}
}
