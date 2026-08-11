package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
	"github.com/jarviisha/darkvoid/internal/infrastructure/database"
)

// CommentLikeRepository handles DB access for comment likes.
type CommentLikeRepository struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewCommentLikeRepository creates a new CommentLikeRepository.
func NewCommentLikeRepository(pool *pgxpool.Pool) *CommentLikeRepository {
	return &CommentLikeRepository{pool: pool, queries: db.New(pool)}
}

// Toggle serializes mutations for one (user, comment) pair with a transaction-
// scoped advisory lock, then returns the state committed by that transaction.
func (r *CommentLikeRepository) Toggle(ctx context.Context, userID, commentID uuid.UUID) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, database.MapDBError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	queries := db.New(tx)
	params := db.LockCommentLikeParams{UserID: userID.String(), CommentID: commentID.String()}
	if lockErr := queries.LockCommentLike(ctx, params); lockErr != nil {
		return false, database.MapDBError(lockErr)
	}

	liked, err := queries.IsCommentLiked(ctx, db.IsCommentLikedParams{UserID: userID, CommentID: commentID})
	if err != nil {
		return false, database.MapDBError(err)
	}
	if liked {
		err = queries.UnlikeComment(ctx, db.UnlikeCommentParams{UserID: userID, CommentID: commentID})
	} else {
		err = queries.LikeComment(ctx, db.LikeCommentParams{UserID: userID, CommentID: commentID})
	}
	if err != nil {
		return false, database.MapDBError(err)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return false, database.MapDBError(commitErr)
	}
	return !liked, nil
}

// Like inserts a like for the given user/comment pair. Idempotent (ON CONFLICT DO NOTHING).
func (r *CommentLikeRepository) Like(ctx context.Context, userID, commentID uuid.UUID) error {
	return database.MapDBError(r.queries.LikeComment(ctx, db.LikeCommentParams{
		UserID:    userID,
		CommentID: commentID,
	}))
}

// Unlike removes the like for the given user/comment pair.
func (r *CommentLikeRepository) Unlike(ctx context.Context, userID, commentID uuid.UUID) error {
	return database.MapDBError(r.queries.UnlikeComment(ctx, db.UnlikeCommentParams{
		UserID:    userID,
		CommentID: commentID,
	}))
}

// IsLiked returns whether the user has liked the given comment.
func (r *CommentLikeRepository) IsLiked(ctx context.Context, userID, commentID uuid.UUID) (bool, error) {
	ok, err := r.queries.IsCommentLiked(ctx, db.IsCommentLikedParams{
		UserID:    userID,
		CommentID: commentID,
	})
	if err != nil {
		return false, database.MapDBError(err)
	}
	return ok, nil
}

// GetLikedCommentIDs returns which comment IDs from the given slice the user has liked.
func (r *CommentLikeRepository) GetLikedCommentIDs(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) ([]uuid.UUID, error) {
	ids, err := r.queries.GetLikedCommentIDs(ctx, db.GetLikedCommentIDsParams{
		UserID:  userID,
		Column2: commentIDs,
	})
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return ids, nil
}
