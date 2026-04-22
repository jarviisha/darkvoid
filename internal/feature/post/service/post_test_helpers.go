package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	pkgerrors "github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// Transaction mocks
// --------------------------------------------------------------------------

// mockTx is a no-op pgx.Tx used in unit tests.
type mockTx struct{}

func (m *mockTx) Begin(ctx context.Context) (pgx.Tx, error) { return m, nil }
func (m *mockTx) Commit(ctx context.Context) error          { return nil }
func (m *mockTx) Rollback(ctx context.Context) error        { return nil }
func (m *mockTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	panic("not implemented")
}
func (m *mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	panic("not implemented")
}
func (m *mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	panic("not implemented")
}
func (m *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	panic("not implemented")
}
func (m *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	panic("not implemented")
}
func (m *mockTx) LargeObjects() pgx.LargeObjects { panic("not implemented") }
func (m *mockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	panic("not implemented")
}
func (m *mockTx) Conn() *pgx.Conn { panic("not implemented") }

// mockTxBeginner returns a mockTx for every Begin call.
type mockTxBeginner struct{}

func (m *mockTxBeginner) Begin(ctx context.Context) (pgx.Tx, error) { return &mockTx{}, nil }

// --------------------------------------------------------------------------
// Repository mocks
// --------------------------------------------------------------------------

type mockPostRepo struct {
	create                func(ctx context.Context, authorID uuid.UUID, content string, visibility entity.Visibility) (*entity.Post, error)
	getByID               func(ctx context.Context, id uuid.UUID) (*entity.Post, error)
	getByAuthorWithCursor func(ctx context.Context, authorID uuid.UUID, cursorCreatedAt pgtype.Timestamptz, cursorPostID uuid.UUID, visibilityFilter string, limit int32) ([]*entity.Post, error)
	update                func(ctx context.Context, id uuid.UUID, content string, visibility entity.Visibility) (*entity.Post, error)
	delete                func(ctx context.Context, id uuid.UUID) error
}

func (m *mockPostRepo) WithTx(_ pgx.Tx) postRepo { return m }
func (m *mockPostRepo) Create(ctx context.Context, authorID uuid.UUID, content string, visibility entity.Visibility) (*entity.Post, error) {
	if m.create != nil {
		return m.create(ctx, authorID, content, visibility)
	}
	return &entity.Post{ID: uuid.New(), AuthorID: authorID, Content: content, Visibility: visibility, CreatedAt: time.Now()}, nil
}
func (m *mockPostRepo) GetByID(ctx context.Context, id uuid.UUID) (*entity.Post, error) {
	if m.getByID != nil {
		return m.getByID(ctx, id)
	}
	return nil, pkgerrors.ErrNotFound
}
func (m *mockPostRepo) GetByAuthorWithCursor(ctx context.Context, authorID uuid.UUID, cursorCreatedAt pgtype.Timestamptz, cursorPostID uuid.UUID, visibilityFilter string, limit int32) ([]*entity.Post, error) {
	if m.getByAuthorWithCursor != nil {
		return m.getByAuthorWithCursor(ctx, authorID, cursorCreatedAt, cursorPostID, visibilityFilter, limit)
	}
	return nil, nil
}
func (m *mockPostRepo) Update(ctx context.Context, id uuid.UUID, content string, visibility entity.Visibility) (*entity.Post, error) {
	if m.update != nil {
		return m.update(ctx, id, content, visibility)
	}
	return nil, pkgerrors.ErrNotFound
}
func (m *mockPostRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if m.delete != nil {
		return m.delete(ctx, id)
	}
	return nil
}

type mockMediaRepo struct {
	add             func(ctx context.Context, postID uuid.UUID, key, mediaType string, position int32) (*entity.PostMedia, error)
	getByPost       func(ctx context.Context, postID uuid.UUID) ([]*entity.PostMedia, error)
	getByPostsBatch func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]*entity.PostMedia, error)
}

func (m *mockMediaRepo) WithTx(_ pgx.Tx) mediaRepo { return m }
func (m *mockMediaRepo) Add(ctx context.Context, postID uuid.UUID, key, mediaType string, position int32) (*entity.PostMedia, error) {
	if m.add != nil {
		return m.add(ctx, postID, key, mediaType, position)
	}
	return &entity.PostMedia{ID: uuid.New(), PostID: postID, MediaKey: key, MediaType: mediaType}, nil
}
func (m *mockMediaRepo) GetByPost(ctx context.Context, postID uuid.UUID) ([]*entity.PostMedia, error) {
	if m.getByPost != nil {
		return m.getByPost(ctx, postID)
	}
	return nil, nil
}
func (m *mockMediaRepo) GetByPostsBatch(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]*entity.PostMedia, error) {
	if m.getByPostsBatch != nil {
		return m.getByPostsBatch(ctx, postIDs)
	}
	return make(map[uuid.UUID][]*entity.PostMedia), nil
}

type mockLikeRepo struct {
	like            func(ctx context.Context, userID, postID uuid.UUID) error
	unlike          func(ctx context.Context, userID, postID uuid.UUID) error
	isLiked         func(ctx context.Context, userID, postID uuid.UUID) (bool, error)
	count           func(ctx context.Context, postID uuid.UUID) (int64, error)
	getLikedPostIDs func(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) ([]uuid.UUID, error)
}

func (m *mockLikeRepo) Like(ctx context.Context, userID, postID uuid.UUID) error {
	if m.like != nil {
		return m.like(ctx, userID, postID)
	}
	return nil
}
func (m *mockLikeRepo) Unlike(ctx context.Context, userID, postID uuid.UUID) error {
	if m.unlike != nil {
		return m.unlike(ctx, userID, postID)
	}
	return nil
}
func (m *mockLikeRepo) IsLiked(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	if m.isLiked != nil {
		return m.isLiked(ctx, userID, postID)
	}
	return false, nil
}
func (m *mockLikeRepo) Count(ctx context.Context, postID uuid.UUID) (int64, error) {
	if m.count != nil {
		return m.count(ctx, postID)
	}
	return 0, nil
}
func (m *mockLikeRepo) GetLikedPostIDs(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) ([]uuid.UUID, error) {
	if m.getLikedPostIDs != nil {
		return m.getLikedPostIDs(ctx, userID, postIDs)
	}
	return nil, nil
}

// --------------------------------------------------------------------------
// Helper functions
// --------------------------------------------------------------------------

// newPostService creates a PostService with the given mocks for testing.
func newPostService(pr postRepo, mr mediaRepo, lr likeRepo) *PostService {
	return &PostService{pool: &mockTxBeginner{}, postRepo: pr, mediaRepo: mr, likeRepo: lr}
}

// samplePost returns a sample post entity for testing.
func samplePost(authorID uuid.UUID) *entity.Post {
	return &entity.Post{
		ID:         uuid.New(),
		AuthorID:   authorID,
		Content:    "Hello world",
		Visibility: entity.VisibilityPublic,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

// --------------------------------------------------------------------------
// Additional mocks for enrichment and mention tests
// --------------------------------------------------------------------------

type mockUserReader struct {
	getAuthorsByIDs func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error)
}

func (m *mockUserReader) GetAuthorsByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
	if m.getAuthorsByIDs != nil {
		return m.getAuthorsByIDs(ctx, ids)
	}
	return make(map[uuid.UUID]*entity.Author), nil
}

type mockFollowChecker struct {
	isFollowing func(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error)
}

func (m *mockFollowChecker) IsFollowing(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
	if m.isFollowing != nil {
		return m.isFollowing(ctx, followerID, followeeID)
	}
	return false, nil
}

type mockHashtagRepo struct {
	getNamesByPostIDs func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]string, error)
}

func (m *mockHashtagRepo) WithTx(_ pgx.Tx) hashtagRepo { return m }
func (m *mockHashtagRepo) UpsertAndLink(_ context.Context, _ uuid.UUID, _ []string) error {
	return nil
}
func (m *mockHashtagRepo) ReplaceForPost(_ context.Context, _ uuid.UUID, _ []string) error {
	return nil
}
func (m *mockHashtagRepo) GetNamesByPostIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]string, error) {
	if m.getNamesByPostIDs != nil {
		return m.getNamesByPostIDs(ctx, postIDs)
	}
	return make(map[uuid.UUID][]string), nil
}
func (m *mockHashtagRepo) GetTrending(_ context.Context, _ int32) ([]*entity.TrendingHashtag, error) {
	return nil, nil
}
func (m *mockHashtagRepo) GetPostsByHashtag(_ context.Context, _ string, _ pgtype.Timestamptz, _ uuid.UUID, _ int32) ([]*entity.Post, error) {
	return nil, nil
}
func (m *mockHashtagRepo) SearchByPrefix(_ context.Context, _ string, _ int32) ([]string, error) {
	return nil, nil
}

type mockMentionRepo struct {
	withTx       func(tx pgx.Tx) mentionRepo
	insert       func(ctx context.Context, postID, userID uuid.UUID) error
	deleteByPost func(ctx context.Context, postID uuid.UUID) error
	getByPost    func(ctx context.Context, postID uuid.UUID) ([]uuid.UUID, error)
	getBatch     func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
}

func (m *mockMentionRepo) WithTx(tx pgx.Tx) mentionRepo {
	if m.withTx != nil {
		return m.withTx(tx)
	}
	return m
}

func (m *mockMentionRepo) Insert(ctx context.Context, postID, userID uuid.UUID) error {
	if m.insert != nil {
		return m.insert(ctx, postID, userID)
	}
	return nil
}

func (m *mockMentionRepo) DeleteByPost(ctx context.Context, postID uuid.UUID) error {
	if m.deleteByPost != nil {
		return m.deleteByPost(ctx, postID)
	}
	return nil
}

func (m *mockMentionRepo) GetByPost(ctx context.Context, postID uuid.UUID) ([]uuid.UUID, error) {
	if m.getByPost != nil {
		return m.getByPost(ctx, postID)
	}
	return nil, nil
}

func (m *mockMentionRepo) GetBatch(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	if m.getBatch != nil {
		return m.getBatch(ctx, postIDs)
	}
	return make(map[uuid.UUID][]uuid.UUID), nil
}

type mockNotificationEmitter struct {
	emitMention func(ctx context.Context, actorID, recipientID, postID uuid.UUID) error
}

func (m *mockNotificationEmitter) EmitMention(ctx context.Context, actorID, recipientID, postID uuid.UUID) error {
	if m.emitMention != nil {
		return m.emitMention(ctx, actorID, recipientID, postID)
	}
	return nil
}
