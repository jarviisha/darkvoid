ALTER TABLE usr.refresh_tokens ADD COLUMN token VARCHAR(255);
UPDATE usr.refresh_tokens SET token = token_hash;
ALTER TABLE usr.refresh_tokens ALTER COLUMN token SET NOT NULL;

DROP INDEX IF EXISTS idx_usr_refresh_tokens_token_hash;
ALTER TABLE usr.refresh_tokens DROP COLUMN token_hash;
CREATE UNIQUE INDEX idx_usr_refresh_tokens_token ON usr.refresh_tokens(token);
