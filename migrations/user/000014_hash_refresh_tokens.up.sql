CREATE EXTENSION IF NOT EXISTS pgcrypto;

ALTER TABLE usr.refresh_tokens ADD COLUMN token_hash CHAR(64);
UPDATE usr.refresh_tokens
SET token_hash = encode(digest(token, 'sha256'), 'hex');
ALTER TABLE usr.refresh_tokens ALTER COLUMN token_hash SET NOT NULL;

DROP INDEX IF EXISTS idx_usr_refresh_tokens_token;
ALTER TABLE usr.refresh_tokens DROP COLUMN token;
CREATE UNIQUE INDEX idx_usr_refresh_tokens_token_hash
    ON usr.refresh_tokens(token_hash);
