-- name: SearchPosts :many
-- SearchPosts performs a full-text search over public posts using tsvector.
SELECT id, author_id, content, visibility, created_at, updated_at, deleted_at, like_count, comment_count
FROM post.posts
WHERE deleted_at IS NULL
  AND visibility = 'public'
  AND search_vector @@ plainto_tsquery('english', sqlc.arg('query')::text)
ORDER BY ts_rank(search_vector, plainto_tsquery('english', sqlc.arg('query')::text)) DESC, created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');
