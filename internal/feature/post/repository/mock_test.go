package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
)

// mockQuerier implements db.Querier for unit tests: sqlc drives production,
// this drives the tests. Set only the function fields the test under execution
// exercises.
//
// Unlike the user context's mockQuerier, an unset field does not return zero
// values — the embedded db.Querier is nil, so any query the test did not stub
// panics on a nil dereference and the stack trace names the method. Silent
// zero values let a test pass while the repository never issued the query it
// was meant to; here that cannot happen.
type mockQuerier struct {
	db.Querier

	addCommentMedia                func(context.Context, db.AddCommentMediaParams) (db.PostCommentMedium, error)
	addPostMedia                   func(context.Context, db.AddPostMediaParams) (db.PostPostMedium, error)
	countCommentsByPost            func(context.Context, uuid.UUID) (int64, error)
	countLikes                     func(context.Context, uuid.UUID) (int64, error)
	createComment                  func(context.Context, db.CreateCommentParams) (db.PostComment, error)
	createPost                     func(context.Context, db.CreatePostParams) (db.PostPost, error)
	deleteAllCommentMedia          func(context.Context, uuid.UUID) error
	deleteAllPostMedia             func(context.Context, uuid.UUID) error
	deleteComment                  func(context.Context, uuid.UUID) error
	deleteCommentMentionsByComment func(context.Context, uuid.UUID) error
	deleteMentionsByPost           func(context.Context, uuid.UUID) error
	deletePost                     func(context.Context, uuid.UUID) error
	deletePostMedia                func(context.Context, db.DeletePostMediaParams) error
	getCommentByID                 func(context.Context, uuid.UUID) (db.PostComment, error)
	getCommentMedia                func(context.Context, uuid.UUID) ([]db.PostCommentMedium, error)
	getCommentMediaBatch           func(context.Context, []uuid.UUID) ([]db.PostCommentMedium, error)
	getCommentMentionsByComment    func(context.Context, uuid.UUID) ([]db.PostCommentMention, error)
	getCommentMentionsBatch        func(context.Context, []uuid.UUID) ([]db.PostCommentMention, error)
	getCommentsByPost              func(context.Context, db.GetCommentsByPostParams) ([]db.PostComment, error)
	getDiscoverWithCursor          func(context.Context, db.GetDiscoverWithCursorParams) ([]db.PostPost, error)
	getFollowingPostsWithCursor    func(context.Context, db.GetFollowingPostsWithCursorParams) ([]db.GetFollowingPostsWithCursorRow, error)
	getHashtagsByPostID            func(context.Context, uuid.UUID) ([]db.PostHashtag, error)
	getHashtagsByPostIDs           func(context.Context, []uuid.UUID) ([]db.GetHashtagsByPostIDsRow, error)
	getLikedCommentIDs             func(context.Context, db.GetLikedCommentIDsParams) ([]uuid.UUID, error)
	getLikedPostIDs                func(context.Context, db.GetLikedPostIDsParams) ([]uuid.UUID, error)
	getMentionsBatch               func(context.Context, []uuid.UUID) ([]db.PostPostMention, error)
	getMentionsByPost              func(context.Context, uuid.UUID) ([]db.PostPostMention, error)
	getPostByID                    func(context.Context, uuid.UUID) (db.PostPost, error)
	getPostMedia                   func(context.Context, uuid.UUID) ([]db.PostPostMedium, error)
	getPostsByHashtagWithCursor    func(context.Context, db.GetPostsByHashtagWithCursorParams) ([]db.GetPostsByHashtagWithCursorRow, error)
	getReplies                     func(context.Context, db.GetRepliesParams) ([]db.PostComment, error)
	getRepliesPreview              func(context.Context, db.GetRepliesPreviewParams) ([]db.PostComment, error)
	getReplyCountsBatch            func(context.Context, []uuid.UUID) ([]db.GetReplyCountsBatchRow, error)
	getTrendingHashtags            func(context.Context, int32) ([]db.GetTrendingHashtagsRow, error)
	getTrendingPosts               func(context.Context, int32) ([]db.PostPost, error)
	getUserPostsWithCursor         func(context.Context, db.GetUserPostsWithCursorParams) ([]db.PostPost, error)
	insertCommentMention           func(context.Context, db.InsertCommentMentionParams) error
	insertMention                  func(context.Context, db.InsertMentionParams) error
	isCommentLiked                 func(context.Context, db.IsCommentLikedParams) (bool, error)
	isLiked                        func(context.Context, db.IsLikedParams) (bool, error)
	likeComment                    func(context.Context, db.LikeCommentParams) error
	likePost                       func(context.Context, db.LikePostParams) error
	searchHashtagsByPrefix         func(context.Context, db.SearchHashtagsByPrefixParams) ([]string, error)
	searchPosts                    func(context.Context, db.SearchPostsParams) ([]db.SearchPostsRow, error)
	unlikeComment                  func(context.Context, db.UnlikeCommentParams) error
	unlikePost                     func(context.Context, db.UnlikePostParams) error
	updatePost                     func(context.Context, db.UpdatePostParams) (db.PostPost, error)
}

func (m *mockQuerier) AddCommentMedia(ctx context.Context, arg db.AddCommentMediaParams) (db.PostCommentMedium, error) {
	return m.addCommentMedia(ctx, arg)
}

func (m *mockQuerier) AddPostMedia(ctx context.Context, arg db.AddPostMediaParams) (db.PostPostMedium, error) {
	return m.addPostMedia(ctx, arg)
}

func (m *mockQuerier) CountCommentsByPost(ctx context.Context, postID uuid.UUID) (int64, error) {
	return m.countCommentsByPost(ctx, postID)
}

func (m *mockQuerier) CountLikes(ctx context.Context, postID uuid.UUID) (int64, error) {
	return m.countLikes(ctx, postID)
}

func (m *mockQuerier) CreateComment(ctx context.Context, arg db.CreateCommentParams) (db.PostComment, error) {
	return m.createComment(ctx, arg)
}

func (m *mockQuerier) CreatePost(ctx context.Context, arg db.CreatePostParams) (db.PostPost, error) {
	return m.createPost(ctx, arg)
}

func (m *mockQuerier) DeleteAllCommentMedia(ctx context.Context, commentID uuid.UUID) error {
	return m.deleteAllCommentMedia(ctx, commentID)
}

func (m *mockQuerier) DeleteAllPostMedia(ctx context.Context, postID uuid.UUID) error {
	return m.deleteAllPostMedia(ctx, postID)
}

func (m *mockQuerier) DeleteComment(ctx context.Context, id uuid.UUID) error {
	return m.deleteComment(ctx, id)
}

func (m *mockQuerier) DeleteCommentMentionsByComment(ctx context.Context, commentID uuid.UUID) error {
	return m.deleteCommentMentionsByComment(ctx, commentID)
}

func (m *mockQuerier) DeleteMentionsByPost(ctx context.Context, postID uuid.UUID) error {
	return m.deleteMentionsByPost(ctx, postID)
}

func (m *mockQuerier) DeletePost(ctx context.Context, id uuid.UUID) error {
	return m.deletePost(ctx, id)
}

func (m *mockQuerier) DeletePostMedia(ctx context.Context, arg db.DeletePostMediaParams) error {
	return m.deletePostMedia(ctx, arg)
}

func (m *mockQuerier) GetCommentByID(ctx context.Context, id uuid.UUID) (db.PostComment, error) {
	return m.getCommentByID(ctx, id)
}

func (m *mockQuerier) GetCommentMedia(ctx context.Context, commentID uuid.UUID) ([]db.PostCommentMedium, error) {
	return m.getCommentMedia(ctx, commentID)
}

func (m *mockQuerier) GetCommentMediaBatch(ctx context.Context, ids []uuid.UUID) ([]db.PostCommentMedium, error) {
	return m.getCommentMediaBatch(ctx, ids)
}

func (m *mockQuerier) GetCommentMentionsByComment(ctx context.Context, commentID uuid.UUID) ([]db.PostCommentMention, error) {
	return m.getCommentMentionsByComment(ctx, commentID)
}

func (m *mockQuerier) GetCommentMentionsBatch(ctx context.Context, ids []uuid.UUID) ([]db.PostCommentMention, error) {
	return m.getCommentMentionsBatch(ctx, ids)
}

func (m *mockQuerier) GetCommentsByPost(ctx context.Context, arg db.GetCommentsByPostParams) ([]db.PostComment, error) {
	return m.getCommentsByPost(ctx, arg)
}

func (m *mockQuerier) GetDiscoverWithCursor(ctx context.Context, arg db.GetDiscoverWithCursorParams) ([]db.PostPost, error) {
	return m.getDiscoverWithCursor(ctx, arg)
}

func (m *mockQuerier) GetFollowingPostsWithCursor(ctx context.Context, arg db.GetFollowingPostsWithCursorParams) ([]db.GetFollowingPostsWithCursorRow, error) {
	return m.getFollowingPostsWithCursor(ctx, arg)
}

func (m *mockQuerier) GetHashtagsByPostID(ctx context.Context, postID uuid.UUID) ([]db.PostHashtag, error) {
	return m.getHashtagsByPostID(ctx, postID)
}

func (m *mockQuerier) GetHashtagsByPostIDs(ctx context.Context, ids []uuid.UUID) ([]db.GetHashtagsByPostIDsRow, error) {
	return m.getHashtagsByPostIDs(ctx, ids)
}

func (m *mockQuerier) GetLikedCommentIDs(ctx context.Context, arg db.GetLikedCommentIDsParams) ([]uuid.UUID, error) {
	return m.getLikedCommentIDs(ctx, arg)
}

func (m *mockQuerier) GetLikedPostIDs(ctx context.Context, arg db.GetLikedPostIDsParams) ([]uuid.UUID, error) {
	return m.getLikedPostIDs(ctx, arg)
}

func (m *mockQuerier) GetMentionsBatch(ctx context.Context, ids []uuid.UUID) ([]db.PostPostMention, error) {
	return m.getMentionsBatch(ctx, ids)
}

func (m *mockQuerier) GetMentionsByPost(ctx context.Context, postID uuid.UUID) ([]db.PostPostMention, error) {
	return m.getMentionsByPost(ctx, postID)
}

func (m *mockQuerier) GetPostByID(ctx context.Context, id uuid.UUID) (db.PostPost, error) {
	return m.getPostByID(ctx, id)
}

func (m *mockQuerier) GetPostMedia(ctx context.Context, postID uuid.UUID) ([]db.PostPostMedium, error) {
	return m.getPostMedia(ctx, postID)
}

func (m *mockQuerier) GetPostsByHashtagWithCursor(ctx context.Context, arg db.GetPostsByHashtagWithCursorParams) ([]db.GetPostsByHashtagWithCursorRow, error) {
	return m.getPostsByHashtagWithCursor(ctx, arg)
}

func (m *mockQuerier) GetReplies(ctx context.Context, arg db.GetRepliesParams) ([]db.PostComment, error) {
	return m.getReplies(ctx, arg)
}

func (m *mockQuerier) GetRepliesPreview(ctx context.Context, arg db.GetRepliesPreviewParams) ([]db.PostComment, error) {
	return m.getRepliesPreview(ctx, arg)
}

func (m *mockQuerier) GetReplyCountsBatch(ctx context.Context, ids []uuid.UUID) ([]db.GetReplyCountsBatchRow, error) {
	return m.getReplyCountsBatch(ctx, ids)
}

func (m *mockQuerier) GetTrendingHashtags(ctx context.Context, limit int32) ([]db.GetTrendingHashtagsRow, error) {
	return m.getTrendingHashtags(ctx, limit)
}

func (m *mockQuerier) GetTrendingPosts(ctx context.Context, limit int32) ([]db.PostPost, error) {
	return m.getTrendingPosts(ctx, limit)
}

func (m *mockQuerier) GetUserPostsWithCursor(ctx context.Context, arg db.GetUserPostsWithCursorParams) ([]db.PostPost, error) {
	return m.getUserPostsWithCursor(ctx, arg)
}

func (m *mockQuerier) InsertCommentMention(ctx context.Context, arg db.InsertCommentMentionParams) error {
	return m.insertCommentMention(ctx, arg)
}

func (m *mockQuerier) InsertMention(ctx context.Context, arg db.InsertMentionParams) error {
	return m.insertMention(ctx, arg)
}

func (m *mockQuerier) IsCommentLiked(ctx context.Context, arg db.IsCommentLikedParams) (bool, error) {
	return m.isCommentLiked(ctx, arg)
}

func (m *mockQuerier) IsLiked(ctx context.Context, arg db.IsLikedParams) (bool, error) {
	return m.isLiked(ctx, arg)
}

func (m *mockQuerier) LikeComment(ctx context.Context, arg db.LikeCommentParams) error {
	return m.likeComment(ctx, arg)
}

func (m *mockQuerier) LikePost(ctx context.Context, arg db.LikePostParams) error {
	return m.likePost(ctx, arg)
}

func (m *mockQuerier) SearchHashtagsByPrefix(ctx context.Context, arg db.SearchHashtagsByPrefixParams) ([]string, error) {
	return m.searchHashtagsByPrefix(ctx, arg)
}

func (m *mockQuerier) SearchPosts(ctx context.Context, arg db.SearchPostsParams) ([]db.SearchPostsRow, error) {
	return m.searchPosts(ctx, arg)
}

func (m *mockQuerier) UnlikeComment(ctx context.Context, arg db.UnlikeCommentParams) error {
	return m.unlikeComment(ctx, arg)
}

func (m *mockQuerier) UnlikePost(ctx context.Context, arg db.UnlikePostParams) error {
	return m.unlikePost(ctx, arg)
}

func (m *mockQuerier) UpdatePost(ctx context.Context, arg db.UpdatePostParams) (db.PostPost, error) {
	return m.updatePost(ctx, arg)
}

// ---------------------------------------------------------------------------
// db.DBTX — the raw-query seam used by the batch reads sqlc cannot generate
// ---------------------------------------------------------------------------

type mockDBTX struct {
	query func(context.Context, string, ...interface{}) (pgx.Rows, error)
}

func (m *mockDBTX) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	panic("mockDBTX.Exec not stubbed")
}

func (m *mockDBTX) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return m.query(ctx, sql, args...)
}

func (m *mockDBTX) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	panic("mockDBTX.QueryRow not stubbed")
}

// mockRows replays a fixed set of column values through pgx.Rows. Each scan
// assigns into the destinations the repository passes, which is what makes the
// hand-written row mapping reachable without a database.
type mockRows struct {
	pgx.Rows

	scans  []func(dest ...any) error
	err    error
	pos    int
	closed bool
}

func (m *mockRows) Next() bool {
	if m.pos >= len(m.scans) {
		return false
	}
	m.pos++
	return true
}

func (m *mockRows) Scan(dest ...any) error { return m.scans[m.pos-1](dest...) }

func (m *mockRows) Err() error { return m.err }

func (m *mockRows) Close() { m.closed = true }
