-- name: CreateRefreshToken :one
INSERT INTO usr.refresh_tokens (
    token_hash,
    user_id,
    expires_at
) VALUES (
    $1, $2, $3
) RETURNING *;

-- name: GetRefreshTokenByToken :one
SELECT * FROM usr.refresh_tokens
WHERE token_hash = $1 AND is_revoked = false
LIMIT 1;

-- name: RevokeRefreshToken :one
UPDATE usr.refresh_tokens
SET is_revoked = true, revoked_at = NOW()
WHERE token_hash = $1 AND is_revoked = false
RETURNING id;

-- name: ConsumeRefreshToken :one
UPDATE usr.refresh_tokens
SET is_revoked = true, revoked_at = NOW()
WHERE token_hash = $1
  AND is_revoked = false
  AND expires_at > NOW()
RETURNING user_id;

-- name: RevokeAllUserRefreshTokens :exec
UPDATE usr.refresh_tokens
SET is_revoked = true, revoked_at = NOW()
WHERE user_id = $1 AND is_revoked = false;

-- name: DeleteExpiredRefreshTokens :exec
DELETE FROM usr.refresh_tokens
WHERE expires_at < NOW();

-- name: GetActiveRefreshTokensByUserID :many
SELECT * FROM usr.refresh_tokens
WHERE user_id = $1 AND is_revoked = false AND expires_at > NOW()
ORDER BY created_at DESC;
