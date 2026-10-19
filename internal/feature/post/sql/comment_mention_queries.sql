-- name: InsertCommentMention :exec
INSERT INTO post.comment_mentions (comment_id, user_id, position)
VALUES ($1, $2, $3)
ON CONFLICT (comment_id, user_id) DO NOTHING;

-- name: DeleteCommentMentionsByComment :exec
DELETE FROM post.comment_mentions WHERE comment_id = $1;

-- name: GetCommentMentionsByComment :many
SELECT comment_id, user_id, created_at, position FROM post.comment_mentions WHERE comment_id = $1
ORDER BY position;

-- name: GetCommentMentionsBatch :many
SELECT comment_id, user_id, created_at, position FROM post.comment_mentions WHERE comment_id = ANY($1::uuid[])
ORDER BY comment_id, position;
