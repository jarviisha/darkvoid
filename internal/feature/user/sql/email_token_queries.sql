-- name: CreateEmailToken :one
INSERT INTO usr.email_tokens (user_id, token, type, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetEmailTokenByToken :one
SELECT * FROM usr.email_tokens
WHERE token = $1;

-- name: ClaimEmailToken :execrows
-- Marks the token used only if nothing has yet. Zero rows means another
-- request redeemed it first.
UPDATE usr.email_tokens
SET used_at = NOW()
WHERE id = $1 AND used_at IS NULL;

-- name: DeleteEmailTokensByUserAndType :exec
DELETE FROM usr.email_tokens
WHERE user_id = $1 AND type = $2;

-- name: DeleteExpiredEmailTokens :exec
DELETE FROM usr.email_tokens
WHERE expires_at < NOW();
