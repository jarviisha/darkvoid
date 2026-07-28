-- The subject pool each generation draws from at random, previously the
-- compile-time `topics` slice in cmd/bot/personas.go. Runtime data now, so an
-- admin can steer what the bots talk about.
CREATE TABLE bot.topics (
    id         UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    -- UNIQUE so re-adding an existing subject is rejected rather than skewing the
    -- random draw toward the duplicate.
    content    TEXT        NOT NULL UNIQUE,
    -- Retiring a topic keeps it out of the draw without losing the bot.runs rows
    -- that referenced it, and without a delete an admin might not want.
    enabled    BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- GET /bot/plan reads only the enabled topics, on every tick.
CREATE INDEX idx_bot_topics_enabled ON bot.topics(created_at) WHERE enabled;
