-- ─── Personas ────────────────────────────────────────────────────────────────

-- name: ListBots :many
SELECT * FROM bot.bots ORDER BY username;

-- name: GetBot :one
SELECT * FROM bot.bots WHERE id = $1;

-- name: GetBotByUsername :one
SELECT * FROM bot.bots WHERE username = $1;

-- Serves GET /bot/plan. The limit is bot.config.accounts, and ordering by username
-- keeps the selection stable so a config change is the only thing that moves it.
--
-- Personas with a pending run request sort first, which is load-bearing rather than
-- cosmetic: an operator can ask any persona to post now, but the plan only carries
-- `accounts` of them. Without this, a request for a persona sorting past the cap
-- would sit in the table forever and run-now would silently do nothing.
-- name: ListEnabledBots :many
SELECT * FROM bot.bots
WHERE enabled
ORDER BY (run_requested_at IS NULL), username
LIMIT $1;

-- COALESCE over nullable args makes this a partial update: an omitted field keeps
-- its stored value rather than being cleared.
--
-- Disabling a persona also retires any pending run-now. The plan only carries
-- enabled personas, so the request can no longer fire; leaving it set would make
-- the persona post the instant someone re-enables it, answering a request made
-- however long ago. Done in the same statement so there is no window where a
-- disabled persona still looks like it has a run pending.
-- name: UpdateBot :one
UPDATE bot.bots
SET display_name     = COALESCE(sqlc.narg(display_name), display_name),
    style            = COALESCE(sqlc.narg(style), style),
    enabled          = COALESCE(sqlc.narg(enabled), enabled),
    run_requested_at = CASE
                           WHEN COALESCE(sqlc.narg(enabled), enabled) THEN run_requested_at
                           ELSE NULL
                       END,
    updated_at       = NOW()
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

-- Every editable field is COALESCE'd so an omitted one keeps its stored value.
--
-- updated_by deliberately is not. It records who saved last, so it has to be
-- overwritten on every write — wrapping it in COALESCE would make a caller that
-- omits it silently leave the previous editor's name against someone else's change,
-- which is worse than recording nobody. Every caller does supply it; the service is
-- the only writer and always passes the acting admin.
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

-- Drops runs past the retention window, keeping the log — and therefore the cost of
-- summarising it below — bounded however long the bot runs. Called on each report
-- rather than by a scheduler: in steady state the index finds nothing to delete and
-- it costs well under a millisecond, which is cheaper than owning a cron.
-- name: PruneRuns :execrows
DELETE FROM bot.runs WHERE created_at < $1;

-- Per-persona summary for the status columns of GET /admin/bots. Aggregating in
-- one pass avoids a query per bot in the list handler.
--
-- The cutoff is the same retention window the prune uses, so the summary can never
-- describe a period the log no longer covers. It also keeps this bounded if pruning
-- ever stops — a persona that has gone quiet stops reporting, and with it stops
-- triggering the prune.
--
-- Driving this from bot.bots with a subquery per persona looks like it should be
-- faster and measurably is not: with no rows for a persona the planner walks the
-- whole created_at index looking for a match that never comes. Measured at 210k
-- rows, that shape took 250ms against 45ms for this one. The scan is the right
-- answer; bounding what it scans is the fix.
--
-- The MAX casts are load-bearing: sqlc cannot infer the type of an aggregate with
-- a FILTER clause and emits interface{} without them, pushing a runtime type
-- assertion into the repository.
-- name: ListBotRunStats :many
SELECT bot_id,
       (MAX(created_at) FILTER (WHERE status = 'success'))::timestamptz       AS last_success_at,
       (MAX(created_at) FILTER (WHERE status = 'error'))::timestamptz         AS last_error_at,
       COUNT(*) FILTER (WHERE status = 'success'
                          AND created_at >= NOW() - INTERVAL '24 hours')      AS successes_last_24h,
       COUNT(*) FILTER (WHERE status = 'error'
                          AND created_at >= NOW() - INTERVAL '24 hours')      AS errors_last_24h
FROM bot.runs
WHERE created_at >= sqlc.arg(retention_cutoff)
GROUP BY bot_id;
