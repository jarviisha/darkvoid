-- name: InsertMention :exec
INSERT INTO post.post_mentions (post_id, user_id, position)
VALUES ($1, $2, $3)
ON CONFLICT (post_id, user_id) DO NOTHING;

-- name: DeleteMentionsByPost :exec
DELETE FROM post.post_mentions WHERE post_id = $1;

-- name: GetMentionsByPost :many
SELECT post_id, user_id, created_at, position FROM post.post_mentions WHERE post_id = $1
ORDER BY position;

-- name: GetMentionsBatch :many
SELECT post_id, user_id, created_at, position FROM post.post_mentions WHERE post_id = ANY($1::uuid[])
ORDER BY post_id, position;
