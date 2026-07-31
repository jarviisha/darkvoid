-- Dropping these returns the bot to its compile-time defaults rather than to no
-- behaviour: cmd/bot keeps a built-in default for every one of them, because a plan
-- fetch can fail and the bot still has to be able to generate.
ALTER TABLE bot.config
    DROP COLUMN IF EXISTS gemini_timeout_seconds,
    DROP COLUMN IF EXISTS api_timeout_seconds,
    DROP COLUMN IF EXISTS recent_memory,
    DROP COLUMN IF EXISTS max_tags_per_post,
    DROP COLUMN IF EXISTS temperature,
    DROP COLUMN IF EXISTS prompt_template;
