-- Index housekeeping, driven by pg_stat_user_indexes on a populated database rather
-- than by guessing.
--
-- Both partial indexes were added on the assumption that a query filtering `enabled`
-- would use them. Neither does. bot.bots and bot.topics hold single- to double-digit
-- rows, so the planner seq-scans them whatever indexes exist — measured: the plan
-- query is a Seq Scan + Sort, and idx_bot_topics_enabled had never been scanned at
-- all. idx_bot_bots_enabled stopped being reachable when the plan query started
-- ordering by (run_requested_at IS NULL), username, which its key cannot serve.
--
-- An index that is never chosen is not free: it costs an extra write per insert and
-- update, and it documents an access path that does not exist. If either table ever
-- grows enough to need one, it should be added against a measurement then.
DROP INDEX IF EXISTS bot.idx_bot_bots_enabled;
DROP INDEX IF EXISTS bot.idx_bot_topics_enabled;

-- The runs indexes are used and stay. Their names claim keyset pagination, which
-- this table does not do — the activity log is offset-paginated like the rest of the
-- admin surface. They serve the ORDER BY, so name them for that instead of implying
-- a cursor contract a reader would then go looking for.
ALTER INDEX bot.idx_bot_runs_cursor RENAME TO idx_bot_runs_recent;
ALTER INDEX bot.idx_bot_runs_bot_cursor RENAME TO idx_bot_runs_bot_recent;
