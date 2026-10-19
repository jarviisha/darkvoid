package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
	"github.com/jarviisha/darkvoid/internal/infrastructure/database"
)

// CommentMentionRepository handles persistence of comment mentions.
type CommentMentionRepository struct {
	queries db.Querier
}

// NewCommentMentionRepository creates a new CommentMentionRepository.
func NewCommentMentionRepository(pool *pgxpool.Pool) *CommentMentionRepository {
	return &CommentMentionRepository{queries: db.New(pool)}
}

// Insert adds a mention row for a comment at position, its place in the order
// the author wrote the mentions, ignoring duplicates.
func (r *CommentMentionRepository) Insert(ctx context.Context, commentID, userID uuid.UUID, position int32) error {
	return database.MapDBError(r.queries.InsertCommentMention(ctx, db.InsertCommentMentionParams{
		CommentID: commentID,
		UserID:    userID,
		Position:  position,
	}))
}

// DeleteByComment removes all mentions for a comment.
func (r *CommentMentionRepository) DeleteByComment(ctx context.Context, commentID uuid.UUID) error {
	return database.MapDBError(r.queries.DeleteCommentMentionsByComment(ctx, commentID))
}

// GetByComment returns the user IDs of all users mentioned in a comment, in written order.
func (r *CommentMentionRepository) GetByComment(ctx context.Context, commentID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.queries.GetCommentMentionsByComment(ctx, commentID)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.UserID
	}
	return ids, nil
}

// GetBatch returns mentioned user IDs grouped by comment ID, each in written order.
func (r *CommentMentionRepository) GetBatch(ctx context.Context, commentIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	rows, err := r.queries.GetCommentMentionsBatch(ctx, commentIDs)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	result := make(map[uuid.UUID][]uuid.UUID, len(commentIDs))
	for _, row := range rows {
		result[row.CommentID] = append(result[row.CommentID], row.UserID)
	}
	return result, nil
}
