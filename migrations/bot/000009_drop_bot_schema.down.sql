-- Recreate the schema as it stood after 000008, empty.
--
-- Structure only — the data is gone, and a down migration cannot invent it. This
-- exists so the ladder below it still runs: `make migrate-down` rolls back one
-- step per module, so a down that raised an error here would break rolling back
-- an unrelated module, and one that did nothing would leave 000008..000002's
-- downs referencing tables that no longer exist.
--
-- Consolidated rather than replayed: 000007 dropped idx_bot_bots_enabled and
-- idx_bot_topics_enabled and renamed the two runs indexes, and 000008 added the
-- generation columns, so this is the net shape those migrations left. Comments
-- are kept to the reasons that outlived the tables; the originals are in
-- 000002-000008.

CREATE SCHEMA IF NOT EXISTS bot;

CREATE TABLE bot.bots (
    id               UUID         NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    username         VARCHAR(30)  NOT NULL UNIQUE CHECK (username ~ '^[a-zA-Z0-9_-]{3,30}$'),
    display_name     VARCHAR(100) NOT NULL,
    style            TEXT         NOT NULL,
    enabled          BOOLEAN      NOT NULL DEFAULT TRUE,
    -- usr.users(id), held without a foreign key: every module migrates on its own
    -- version table, so a cross-schema reference would force an ordering the
    -- migrator cannot honour.
    user_id          UUID,
    run_requested_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Single-row table (CHECK id = 1): the settings are a fixed, typed set, so the
-- database can enforce their ranges instead of every writer.
CREATE TABLE bot.config (
    id                     SMALLINT         NOT NULL PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    post_interval_seconds  INTEGER          NOT NULL DEFAULT 30 CHECK (post_interval_seconds > 0),
    accounts               SMALLINT         NOT NULL DEFAULT 3 CHECK (accounts > 0),
    models                 TEXT[]           NOT NULL CHECK (cardinality(models) > 0),
    paused                 BOOLEAN          NOT NULL DEFAULT FALSE,
    updated_by             UUID,
    updated_at             TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    prompt_template        TEXT             NOT NULL DEFAULT '',
    temperature            DOUBLE PRECISION NOT NULL DEFAULT 1.0
        CHECK (temperature >= 0 AND temperature <= 2),
    max_tags_per_post      SMALLINT         NOT NULL DEFAULT 3
        CHECK (max_tags_per_post >= 0 AND max_tags_per_post <= 10),
    recent_memory          SMALLINT         NOT NULL DEFAULT 5
        CHECK (recent_memory >= 0 AND recent_memory <= 50),
    api_timeout_seconds    SMALLINT         NOT NULL DEFAULT 15
        CHECK (api_timeout_seconds > 0 AND api_timeout_seconds <= 300),
    gemini_timeout_seconds SMALLINT         NOT NULL DEFAULT 60
        CHECK (gemini_timeout_seconds > 0 AND gemini_timeout_seconds <= 300)
);

CREATE TABLE bot.topics (
    id         UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    content    TEXT        NOT NULL UNIQUE,
    enabled    BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE bot.runs (
    id         UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    bot_id     UUID        NOT NULL REFERENCES bot.bots(id) ON DELETE CASCADE,
    post_id    UUID,
    model_used TEXT,
    status     VARCHAR(20) NOT NULL CHECK (status IN ('success', 'error')),
    error      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- A run is either a post or a failure, never both and never neither.
    CONSTRAINT bot_runs_outcome_consistent CHECK (
        (status = 'success' AND post_id IS NOT NULL AND error IS NULL)
        OR (status = 'error' AND post_id IS NULL AND error IS NOT NULL)
    )
);

CREATE INDEX idx_bot_runs_recent ON bot.runs(created_at DESC, id DESC);
CREATE INDEX idx_bot_runs_bot_recent ON bot.runs(bot_id, created_at DESC, id DESC);
