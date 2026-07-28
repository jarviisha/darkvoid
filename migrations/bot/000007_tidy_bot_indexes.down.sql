ALTER INDEX bot.idx_bot_runs_bot_recent RENAME TO idx_bot_runs_bot_cursor;
ALTER INDEX bot.idx_bot_runs_recent RENAME TO idx_bot_runs_cursor;

CREATE INDEX idx_bot_topics_enabled ON bot.topics(created_at) WHERE enabled;
CREATE INDEX idx_bot_bots_enabled ON bot.bots(username) WHERE enabled;
