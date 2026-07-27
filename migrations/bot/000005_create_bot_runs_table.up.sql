-- Activity log: one row per attempt the bot reports to POST /bot/runs. This is
-- the only reason an admin can see what the bot is doing — before it, the record
-- lived exclusively in journalctl on whichever host ran the process.
CREATE TABLE bot.runs (
    id         UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    bot_id     UUID        NOT NULL REFERENCES bot.bots(id) ON DELETE CASCADE,
    -- post.posts(id) on success. No foreign key: cross-schema, same reason as
    -- bot.bots.user_id. The admin activity log resolves it to content through the
    -- post service, not a join.
    post_id    UUID,
    -- Which model in bot.config.models actually answered, so quota rotation is
    -- visible rather than inferred.
    model_used TEXT,
    status     VARCHAR(20) NOT NULL CHECK (status IN ('success', 'error')),
    error      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- A run is either a post or a failure, never both and never neither. Without
    -- this an under-populated report would read as a successful post that
    -- produced nothing.
    CONSTRAINT bot_runs_outcome_consistent CHECK (
        (status = 'success' AND post_id IS NOT NULL AND error IS NULL)
        OR (status = 'error' AND post_id IS NULL AND error IS NOT NULL)
    )
);

-- Activity log, newest first: unfiltered for GET /admin/bots/runs and per-bot for
-- the last_run / last_error columns of GET /admin/bots.
CREATE INDEX idx_bot_runs_cursor ON bot.runs(created_at DESC, id DESC);
CREATE INDEX idx_bot_runs_bot_cursor ON bot.runs(bot_id, created_at DESC, id DESC);
