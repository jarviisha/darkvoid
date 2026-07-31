-- The remaining bot knobs that still required a rebuild to change.
--
-- Post interval, account count and the model chain moved out of the environment in
-- 000003, but the things that actually shape what a persona writes — the prompt
-- itself, the sampling temperature, how many tags to ask for, how much of its own
-- recent output it is shown — stayed as compile-time constants in cmd/bot. Tuning a
-- voice therefore meant editing Go, rebuilding, and redeploying the bot host, which
-- is the same cost the environment variables had. They belong next to the knobs they
-- interact with: temperature and the prompt's "1-4 câu" instruction are one setting
-- in two halves, and so are the prompt's tag instruction and the client-side cap.
ALTER TABLE bot.config
    -- The Gemini prompt as a Go text/template, rendered against the persona, topic
    -- and recent-post list. Empty means "use the bot's built-in default": the default
    -- has to exist in cmd/bot regardless (a bot that cannot reach a template cannot
    -- post at all), so storing a copy here on migration would create a second source
    -- of truth that silently goes stale. Empty is also the way back from a bad edit.
    ADD COLUMN prompt_template       TEXT             NOT NULL DEFAULT '',

    -- Sampling temperature handed to generateContent. DOUBLE PRECISION rather than
    -- REAL so it round-trips as a Go float64 without a narrowing conversion at the
    -- repository boundary. The 0..2 range is Gemini's own.
    ADD COLUMN temperature           DOUBLE PRECISION NOT NULL DEFAULT 1.0
        CHECK (temperature >= 0 AND temperature <= 2),

    -- How many tags to ask for and keep. The ceiling is the post service's own tag
    -- limit (validateTags rejects more than 10), so a value above it would produce
    -- posts the API refuses — an operator setting would turn into a run of failures
    -- in the activity log with nothing naming the cause.
    ADD COLUMN max_tags_per_post     SMALLINT         NOT NULL DEFAULT 3
        CHECK (max_tags_per_post >= 0 AND max_tags_per_post <= 10),

    -- How many of a persona's latest posts are echoed back into the prompt as a
    -- repetition guard. 0 disables it. The ceiling is a prompt-size guard, not a
    -- product limit: every entry is another line the model has to read.
    ADD COLUMN recent_memory         SMALLINT         NOT NULL DEFAULT 5
        CHECK (recent_memory >= 0 AND recent_memory <= 50),

    -- Per-request timeouts for the bot's two HTTP clients. Separate because they
    -- bound very different calls: the DarkVoid API answers in milliseconds, while a
    -- generateContent call routinely takes tens of seconds. One shared value would
    -- have to be the larger of the two, which would leave a hung API call holding a
    -- tick for a minute.
    ADD COLUMN api_timeout_seconds    SMALLINT        NOT NULL DEFAULT 15
        CHECK (api_timeout_seconds > 0 AND api_timeout_seconds <= 300),
    ADD COLUMN gemini_timeout_seconds SMALLINT        NOT NULL DEFAULT 60
        CHECK (gemini_timeout_seconds > 0 AND gemini_timeout_seconds <= 300);
