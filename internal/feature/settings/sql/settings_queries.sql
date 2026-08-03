-- ─── Feed settings ───────────────────────────────────────────────────────────
--
-- The row is created by the migration, so both queries assume id = 1 exists.
-- There is no upsert-on-read: a missing row would be a second "no value" case the
-- refresher would have to tell apart from a failed query, and the column defaults
-- already answer both.

-- name: GetFeedSettings :one
SELECT * FROM settings.feed WHERE id = 1;

-- Partial update: every settable column is COALESCE'd against its own value, so a
-- NULL parameter means "unchanged" rather than "clear". That is what lets the
-- admin API accept a body naming one knob without the caller having to read the
-- other nine and send them back — a read-modify-write that would lose a
-- concurrent edit made between the two calls.
--
-- updated_by is deliberately not COALESCE'd, for the same reason as
-- UpdateBotConfig: it records who saved last, so a caller that omits it must not
-- leave the previous editor's name attached to someone else's change. The service
-- is the only writer and always passes the acting admin.
-- name: UpdateFeedSettings :one
UPDATE settings.feed
SET timeline_enabled         = COALESCE(sqlc.narg(timeline_enabled), timeline_enabled),
    timeline_rollout_percent = COALESCE(sqlc.narg(timeline_rollout_percent), timeline_rollout_percent),
    timeline_max_items       = COALESCE(sqlc.narg(timeline_max_items), timeline_max_items),
    timeline_ttl_seconds     = COALESCE(sqlc.narg(timeline_ttl_seconds), timeline_ttl_seconds),
    timeline_refresh_on_miss = COALESCE(sqlc.narg(timeline_refresh_on_miss), timeline_refresh_on_miss),
    fanout_enabled           = COALESCE(sqlc.narg(fanout_enabled), fanout_enabled),
    fanout_max_followers     = COALESCE(sqlc.narg(fanout_max_followers), fanout_max_followers),
    relationship_bonus       = COALESCE(sqlc.narg(relationship_bonus), relationship_bonus),
    recency_scale            = COALESCE(sqlc.narg(recency_scale), recency_scale),
    decay_exponent           = COALESCE(sqlc.narg(decay_exponent), decay_exponent),
    updated_by               = sqlc.narg(updated_by),
    updated_at               = NOW()
WHERE id = 1
RETURNING *;
