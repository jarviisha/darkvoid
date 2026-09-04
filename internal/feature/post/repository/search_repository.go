package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/internal/feature/post/db"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	"github.com/jarviisha/darkvoid/internal/infrastructure/database"
)

// PostSearchRepository queries posts by full-text search.
type PostSearchRepository struct {
	queries db.Querier
}

// NewPostSearchRepository creates a new PostSearchRepository.
func NewPostSearchRepository(pool *pgxpool.Pool) *PostSearchRepository {
	return &PostSearchRepository{queries: db.New(pool)}
}

// SearchByQuery returns public posts whose content matches query, ordered by relevance.
func (r *PostSearchRepository) SearchByQuery(ctx context.Context, query string, limit, offset int32) ([]*entity.Post, error) {
	rows, err := r.queries.SearchPosts(ctx, db.SearchPostsParams{
		Query:  query,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return searchRowsToPosts(rows), nil
}

func searchRowsToPosts(rows []db.SearchPostsRow) []*entity.Post {
	result := make([]*entity.Post, len(rows))
	for i, row := range rows {
		p := &entity.Post{
			ID:           row.ID,
			AuthorID:     row.AuthorID,
			Content:      row.Content,
			Visibility:   entity.Visibility(row.Visibility),
			LikeCount:    row.LikeCount,
			CommentCount: row.CommentCount,
			CreatedAt:    row.CreatedAt.Time,
			UpdatedAt:    row.UpdatedAt.Time,
		}
		if row.DeletedAt.Valid {
			t := row.DeletedAt.Time
			p.DeletedAt = &t
		}
		result[i] = p
	}
	return result
}
