-- name: SearchPosts :many
-- SearchPosts performs a full-text search over public posts using tsvector.
-- The configuration and the normalization here must match the ones
-- post.posts.search_vector is generated with (migrations/post/000001_init.up.sql). They are
-- two halves of one comparison: a query built with a different configuration
-- does not match worse, it matches nothing, and an empty page is
-- indistinguishable from an empty corpus.
SELECT id, author_id, content, visibility, created_at, updated_at, deleted_at, like_count, comment_count
FROM post.posts
WHERE deleted_at IS NULL
  AND visibility = 'public'
  AND search_vector @@ plainto_tsquery('simple', post.immutable_unaccent(sqlc.arg('query')::text))
ORDER BY ts_rank(search_vector, plainto_tsquery('simple', post.immutable_unaccent(sqlc.arg('query')::text))) DESC, created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');
