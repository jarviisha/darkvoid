-- One row per bot persona. This replaces the compile-time `personas` slice in
-- cmd/bot/personas.go so an admin can add, retire, or re-voice a bot without a
-- rebuild and redeploy.
CREATE TABLE bot.bots (
    id               UUID         NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    -- Must satisfy the user-service username rule, since cmd/bot registers the
    -- account through POST /auth/register.
    username         VARCHAR(30)  NOT NULL UNIQUE CHECK (username ~ '^[a-zA-Z0-9_-]{3,30}$'),
    display_name     VARCHAR(100) NOT NULL,
    -- Voice/tone instruction embedded in the Gemini prompt (see buildPrompt).
    style            TEXT         NOT NULL,
    enabled          BOOLEAN      NOT NULL DEFAULT TRUE,
    -- usr.users(id), filled in once cmd/bot has registered the account. Declared
    -- without a foreign key: every module migrates on its own version table, so
    -- a cross-schema reference would force an ordering the migrator can't honour
    -- (post.posts.author_id follows the same rule).
    user_id          UUID,
    -- Set by POST /admin/bots/{id}/run-now, cleared by the bot once it has run.
    -- A nullable timestamp rather than a command table: the request is a single
    -- latest-wins flag, and the timestamp doubles as an audit of when it was asked.
    run_requested_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- GET /bot/plan reads only the enabled personas, on every tick.
CREATE INDEX idx_bot_bots_enabled ON bot.bots(username) WHERE enabled;
