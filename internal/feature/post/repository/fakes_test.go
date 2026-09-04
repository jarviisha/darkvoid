package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
)

// fakeQuerier is the second adapter behind db.Querier: sqlc drives production,
// this drives the tests. Only the methods a test needs are overridden; the
// embedded nil interface makes any other call a panic that names the method,
// so a test can never pass by silently reaching an unstubbed query.
type fakeQuerier struct {
	db.Querier

	createPost     func(context.Context, db.CreatePostParams) (db.PostPost, error)
	getPostByID    func(context.Context, uuid.UUID) (db.PostPost, error)
	updatePost     func(context.Context, db.UpdatePostParams) (db.PostPost, error)
	deletePost     func(context.Context, uuid.UUID) error
	getFollowing   func(context.Context, db.GetFollowingPostsWithCursorParams) ([]db.GetFollowingPostsWithCursorRow, error)
	getTrending    func(context.Context, int32) ([]db.PostPost, error)
	getUserPosts   func(context.Context, db.GetUserPostsWithCursorParams) ([]db.PostPost, error)
	getDiscover    func(context.Context, db.GetDiscoverWithCursorParams) ([]db.PostPost, error)
	searchPosts    func(context.Context, db.SearchPostsParams) ([]db.SearchPostsRow, error)
	mentionsBatch  func(context.Context, []uuid.UUID) ([]db.PostPostMention, error)
	mentionsByPost func(context.Context, uuid.UUID) ([]db.PostPostMention, error)
	cmtMentions    func(context.Context, []uuid.UUID) ([]db.PostCommentMention, error)
	postMedia      func(context.Context, uuid.UUID) ([]db.PostPostMedium, error)
	cmtMediaBatch  func(context.Context, []uuid.UUID) ([]db.PostCommentMedium, error)
	hashtagsByIDs  func(context.Context, []uuid.UUID) ([]db.GetHashtagsByPostIDsRow, error)
	trendingTags   func(context.Context, int32) ([]db.GetTrendingHashtagsRow, error)
	postsByHashtag func(context.Context, db.GetPostsByHashtagWithCursorParams) ([]db.GetPostsByHashtagWithCursorRow, error)
	replyCounts    func(context.Context, []uuid.UUID) ([]db.GetReplyCountsBatchRow, error)
	repliesPreview func(context.Context, db.GetRepliesPreviewParams) ([]db.PostComment, error)
	likedPostIDs   func(context.Context, db.GetLikedPostIDsParams) ([]uuid.UUID, error)
	countLikes     func(context.Context, uuid.UUID) (int64, error)
}

func (f *fakeQuerier) CreatePost(ctx context.Context, arg db.CreatePostParams) (db.PostPost, error) {
	return f.createPost(ctx, arg)
}

func (f *fakeQuerier) GetPostByID(ctx context.Context, id uuid.UUID) (db.PostPost, error) {
	return f.getPostByID(ctx, id)
}

func (f *fakeQuerier) UpdatePost(ctx context.Context, arg db.UpdatePostParams) (db.PostPost, error) {
	return f.updatePost(ctx, arg)
}

func (f *fakeQuerier) DeletePost(ctx context.Context, id uuid.UUID) error {
	return f.deletePost(ctx, id)
}

func (f *fakeQuerier) GetFollowingPostsWithCursor(ctx context.Context, arg db.GetFollowingPostsWithCursorParams) ([]db.GetFollowingPostsWithCursorRow, error) {
	return f.getFollowing(ctx, arg)
}

func (f *fakeQuerier) GetTrendingPosts(ctx context.Context, limit int32) ([]db.PostPost, error) {
	return f.getTrending(ctx, limit)
}

func (f *fakeQuerier) GetUserPostsWithCursor(ctx context.Context, arg db.GetUserPostsWithCursorParams) ([]db.PostPost, error) {
	return f.getUserPosts(ctx, arg)
}

func (f *fakeQuerier) GetDiscoverWithCursor(ctx context.Context, arg db.GetDiscoverWithCursorParams) ([]db.PostPost, error) {
	return f.getDiscover(ctx, arg)
}

func (f *fakeQuerier) SearchPosts(ctx context.Context, arg db.SearchPostsParams) ([]db.SearchPostsRow, error) {
	return f.searchPosts(ctx, arg)
}

func (f *fakeQuerier) GetMentionsBatch(ctx context.Context, ids []uuid.UUID) ([]db.PostPostMention, error) {
	return f.mentionsBatch(ctx, ids)
}

func (f *fakeQuerier) GetMentionsByPost(ctx context.Context, postID uuid.UUID) ([]db.PostPostMention, error) {
	return f.mentionsByPost(ctx, postID)
}

func (f *fakeQuerier) GetCommentMentionsBatch(ctx context.Context, ids []uuid.UUID) ([]db.PostCommentMention, error) {
	return f.cmtMentions(ctx, ids)
}

func (f *fakeQuerier) GetPostMedia(ctx context.Context, postID uuid.UUID) ([]db.PostPostMedium, error) {
	return f.postMedia(ctx, postID)
}

func (f *fakeQuerier) GetCommentMediaBatch(ctx context.Context, ids []uuid.UUID) ([]db.PostCommentMedium, error) {
	return f.cmtMediaBatch(ctx, ids)
}

func (f *fakeQuerier) GetHashtagsByPostIDs(ctx context.Context, ids []uuid.UUID) ([]db.GetHashtagsByPostIDsRow, error) {
	return f.hashtagsByIDs(ctx, ids)
}

func (f *fakeQuerier) GetTrendingHashtags(ctx context.Context, limit int32) ([]db.GetTrendingHashtagsRow, error) {
	return f.trendingTags(ctx, limit)
}

func (f *fakeQuerier) GetPostsByHashtagWithCursor(ctx context.Context, arg db.GetPostsByHashtagWithCursorParams) ([]db.GetPostsByHashtagWithCursorRow, error) {
	return f.postsByHashtag(ctx, arg)
}

func (f *fakeQuerier) GetReplyCountsBatch(ctx context.Context, ids []uuid.UUID) ([]db.GetReplyCountsBatchRow, error) {
	return f.replyCounts(ctx, ids)
}

func (f *fakeQuerier) GetRepliesPreview(ctx context.Context, arg db.GetRepliesPreviewParams) ([]db.PostComment, error) {
	return f.repliesPreview(ctx, arg)
}

func (f *fakeQuerier) GetLikedPostIDs(ctx context.Context, arg db.GetLikedPostIDsParams) ([]uuid.UUID, error) {
	return f.likedPostIDs(ctx, arg)
}

func (f *fakeQuerier) CountLikes(ctx context.Context, postID uuid.UUID) (int64, error) {
	return f.countLikes(ctx, postID)
}

// ---------------------------------------------------------------------------
// db.DBTX — the raw-query seam used by the batch reads sqlc cannot generate
// ---------------------------------------------------------------------------

type fakeDBTX struct {
	query     func(context.Context, string, ...interface{}) (pgx.Rows, error)
	lastSQL   string
	lastArgs  []interface{}
	queryCall int
}

func (f *fakeDBTX) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	panic("fakeDBTX.Exec not stubbed")
}

func (f *fakeDBTX) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	f.queryCall++
	f.lastSQL = sql
	f.lastArgs = args
	return f.query(ctx, sql, args...)
}

func (f *fakeDBTX) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	panic("fakeDBTX.QueryRow not stubbed")
}

// fakeRows replays a fixed set of column values through pgx.Rows. scan assigns
// into the destinations the repository passes, which is what makes the
// hand-written row mapping reachable without a database.
type fakeRows struct {
	pgx.Rows

	scans  []func(dest ...any) error
	err    error
	pos    int
	closed bool
}

func (r *fakeRows) Next() bool {
	if r.pos >= len(r.scans) {
		return false
	}
	r.pos++
	return true
}

func (r *fakeRows) Scan(dest ...any) error { return r.scans[r.pos-1](dest...) }

func (r *fakeRows) Err() error { return r.err }

func (r *fakeRows) Close() { r.closed = true }
