-- ─── Personas ────────────────────────────────────────────────────────────────

-- name: ListBots :many
SELECT * FROM bot.bots ORDER BY username;

-- name: GetBot :one
SELECT * FROM bot.bots WHERE id = $1;

-- name: GetBotByUsername :one
SELECT * FROM bot.bots WHERE username = $1;

-- Serves GET /bot/plan. The limit is bot.config.accounts; ordering by username
-- keeps the selection stable so a config change is the only thing that moves it.
-- name: ListEnabledBots :many
SELECT * FROM bot.bots WHERE enabled ORDER BY username LIMIT $1;

-- COALESCE over nullable args makes this a partial update: an omitted field keeps
-- its stored value rather than being cleared.
-- name: UpdateBot :one
UPDATE bot.bots
SET display_name = COALESCE(sqlc.narg(display_name), display_name),
    style         = COALESCE(sqlc.narg(style), style),
    enabled       = COALESCE(sqlc.narg(enabled), enabled),
    updated_at    = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateBot :one
INSERT INTO bot.bots (username, display_name, style)
VALUES ($1, $2, $3)
RETURNING *;

-- name: DeleteBot :exec
DELETE FROM bot.bots WHERE id = $1;

-- Called by the bot once it has registered or logged in the account, so the admin
-- view can link a persona to the user it posts as.
-- name: LinkBotUser :exec
UPDATE bot.bots SET user_id = $2, updated_at = NOW() WHERE id = $1;

-- ─── Run-now flag ────────────────────────────────────────────────────────────

-- name: RequestBotRun :one
UPDATE bot.bots SET run_requested_at = NOW(), updated_at = NOW() WHERE id = $1 RETURNING *;

-- name: ClearBotRunRequest :exec
UPDATE bot.bots SET run_requested_at = NULL, updated_at = NOW() WHERE id = $1;

-- ─── Config ──────────────────────────────────────────────────────────────────

-- name: GetBotConfig :one
SELECT * FROM bot.config WHERE id = 1;

-- name: UpdateBotConfig :one
UPDATE bot.config
SET post_interval_seconds = COALESCE(sqlc.narg(post_interval_seconds), post_interval_seconds),
    accounts              = COALESCE(sqlc.narg(accounts), accounts),
    models                = COALESCE(sqlc.narg(models), models),
    paused                = COALESCE(sqlc.narg(paused), paused),
    updated_by            = sqlc.narg(updated_by),
    updated_at            = NOW()
WHERE id = 1
RETURNING *;

-- ─── Topics ──────────────────────────────────────────────────────────────────

-- content breaks the created_at tie: the seeded rows all share one timestamp, so
-- created_at alone leaves the admin list in arbitrary order.
-- name: ListTopics :many
SELECT * FROM bot.topics ORDER BY created_at, content;

-- name: ListEnabledTopics :many
SELECT * FROM bot.topics WHERE enabled ORDER BY created_at, content;

-- name: CreateTopic :one
INSERT INTO bot.topics (content) VALUES ($1) RETURNING *;

-- name: SetTopicEnabled :one
UPDATE bot.topics SET enabled = $2 WHERE id = $1 RETURNING *;

-- name: DeleteTopic :exec
DELETE FROM bot.topics WHERE id = $1;

-- ─── Activity log ────────────────────────────────────────────────────────────

-- name: CreateRun :one
INSERT INTO bot.runs (bot_id, post_id, model_used, status, error)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- A NULL bot_id means "every bot", so one query serves both the global log and a
-- single persona's history.
-- name: ListRuns :many
SELECT * FROM bot.runs
WHERE (sqlc.narg(bot_id)::uuid IS NULL OR bot_id = sqlc.narg(bot_id)::uuid)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountRuns :one
SELECT COUNT(*) FROM bot.runs
WHERE (sqlc.narg(bot_id)::uuid IS NULL OR bot_id = sqlc.narg(bot_id)::uuid);

-- Per-persona summary for the status columns of GET /admin/bots. Aggregating in
-- one pass avoids a query per bot in the list handler.
-- name: ListBotRunStats :many
SELECT bot_id,
       MAX(created_at) FILTER (WHERE status = 'success')                    AS last_success_at,
       MAX(created_at) FILTER (WHERE status = 'error')                      AS last_error_at,
       COUNT(*) FILTER (WHERE status = 'success'
                          AND created_at >= NOW() - INTERVAL '24 hours')    AS successes_last_24h,
       COUNT(*) FILTER (WHERE status = 'error'
                          AND created_at >= NOW() - INTERVAL '24 hours')    AS errors_last_24h
FROM bot.runs
GROUP BY bot_id;

-- name: GetLastBotError :one
SELECT * FROM bot.runs
WHERE bot_id = $1 AND status = 'error'
ORDER BY created_at DESC, id DESC
LIMIT 1;
