-- Runtime knobs for the content bot, previously BOT_ACCOUNTS / BOT_POST_INTERVAL
-- / GEMINI_MODELS in the environment. Moving them into the database is what lets
-- the admin API change them; the bot re-reads its plan every tick, so an edit
-- takes effect without a restart.
--
-- Single-row table (CHECK id = 1) rather than a key/value table: the settings are
-- a fixed, typed set, and constraints like accounts > 0 can be enforced by the
-- database instead of in every writer.
CREATE TABLE bot.config (
    id                    SMALLINT    NOT NULL PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    -- Seconds rather than INTERVAL: the wire format is JSON and the consumer is a
    -- Go time.Duration, so an integer avoids a lossy interval round-trip.
    post_interval_seconds INTEGER     NOT NULL DEFAULT 30 CHECK (post_interval_seconds > 0),
    -- How many enabled personas to activate, capped at the number of rows in bot.bots.
    accounts              SMALLINT    NOT NULL DEFAULT 3 CHECK (accounts > 0),
    -- Priority-ordered Gemini model ids. The bot walks the list top-down and
    -- rotates on quota exhaustion (429), so an empty list would stall it.
    models                TEXT[]      NOT NULL CHECK (cardinality(models) > 0),
    -- Global kill switch for POST /admin/bots/pause. Leaves per-bot `enabled` alone
    -- so resuming restores exactly the previous selection.
    paused                BOOLEAN     NOT NULL DEFAULT FALSE,
    -- usr.users(id) of the admin who last saved. No foreign key, same reason as
    -- bot.bots.user_id. NULL for the seeded defaults.
    updated_by            UUID,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
