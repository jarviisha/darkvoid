-- Initial schema baseline. Add future changes in a new numbered migration.

CREATE SCHEMA IF NOT EXISTS usr;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE usr.users (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    username      VARCHAR(50)  UNIQUE NOT NULL,
    email         VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    is_active     BOOLEAN      NOT NULL DEFAULT true,
    display_name  VARCHAR(100) NOT NULL DEFAULT '',
    bio           TEXT,
    avatar_key    VARCHAR(255),
    cover_key     VARCHAR(255),
    website       VARCHAR(255),
    location      VARCHAR(100),
    created_at    TIMESTAMP    NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMP,
    created_by    UUID,
    updated_by    UUID,
    follower_count BIGINT NOT NULL DEFAULT 0,
    following_count BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX idx_usr_users_username   ON usr.users(username);
CREATE INDEX idx_usr_users_email      ON usr.users(email);
CREATE INDEX idx_usr_users_is_active  ON usr.users(is_active);
CREATE INDEX idx_usr_users_created_at ON usr.users(created_at DESC);

CREATE TABLE usr.user_roles (
    user_id     UUID NOT NULL REFERENCES usr.users(id) ON DELETE CASCADE,
    assigned_at TIMESTAMP NOT NULL DEFAULT NOW(),
    assigned_by UUID,
    role        VARCHAR(50) NOT NULL,
    CONSTRAINT usr_user_roles_role_check CHECK (role IN ('admin', 'moderator', 'bot')),
    PRIMARY KEY (user_id, role)
);

CREATE INDEX idx_usr_user_roles_role ON usr.user_roles(role);

CREATE TABLE usr.refresh_tokens (
    id         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID         NOT NULL REFERENCES usr.users(id) ON DELETE CASCADE,
    expires_at TIMESTAMP    NOT NULL,
    created_at TIMESTAMP    NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMP,
    is_revoked BOOLEAN      NOT NULL DEFAULT false,
    token_hash CHAR(64) NOT NULL
);

CREATE INDEX idx_usr_refresh_tokens_user_id    ON usr.refresh_tokens(user_id);
CREATE INDEX idx_usr_refresh_tokens_expires_at ON usr.refresh_tokens(expires_at);
CREATE INDEX idx_usr_refresh_tokens_is_revoked ON usr.refresh_tokens(is_revoked);
CREATE UNIQUE INDEX idx_usr_refresh_tokens_token_hash ON usr.refresh_tokens(token_hash);

CREATE TABLE usr.follows (
    follower_id UUID        NOT NULL,
    followee_id UUID        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (follower_id, followee_id),
    CONSTRAINT chk_no_self_follow CHECK (follower_id <> followee_id)
);

CREATE INDEX idx_usr_follows_follower ON usr.follows(follower_id);
CREATE INDEX idx_usr_follows_followee ON usr.follows(followee_id);

CREATE OR REPLACE FUNCTION usr.update_follow_counts()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE usr.users SET follower_count  = follower_count  + 1 WHERE id = NEW.followee_id;
        UPDATE usr.users SET following_count = following_count + 1 WHERE id = NEW.follower_id;
    ELSIF TG_OP = 'DELETE' THEN
        UPDATE usr.users SET follower_count  = GREATEST(follower_count  - 1, 0) WHERE id = OLD.followee_id;
        UPDATE usr.users SET following_count = GREATEST(following_count - 1, 0) WHERE id = OLD.follower_id;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_follow_counts
AFTER INSERT OR DELETE ON usr.follows
FOR EACH ROW EXECUTE FUNCTION usr.update_follow_counts();

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_users_username_trgm
    ON usr.users USING gin(username gin_trgm_ops)
    WHERE is_active = true;

CREATE INDEX IF NOT EXISTS idx_users_display_name_trgm
    ON usr.users USING gin(display_name gin_trgm_ops)
    WHERE is_active = true;

CREATE TABLE usr.email_tokens (
    id         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID         NOT NULL REFERENCES usr.users(id) ON DELETE CASCADE,
    token      VARCHAR(255) NOT NULL UNIQUE,
    type       VARCHAR(30)  NOT NULL, -- 'verify_email' | 'reset_password'
    expires_at TIMESTAMP    NOT NULL,
    used_at    TIMESTAMP,
    created_at TIMESTAMP    NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_usr_email_tokens_token   ON usr.email_tokens(token);
CREATE INDEX idx_usr_email_tokens_user_id ON usr.email_tokens(user_id, type);

-- Delivery log and suppression list for account email.
--
-- The provider reports delivery outcomes against its own message id and nothing
-- else, so a send has to be recorded under that id before a later bounce can be
-- attributed to a user at all. That is what email_deliveries is for; the
-- suppression list is what makes the bounce actionable.

CREATE TABLE usr.email_deliveries (
    id                  UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID         NOT NULL REFERENCES usr.users(id) ON DELETE CASCADE,
    -- The provider's id for the send: Resend's uuid, or the Message-ID header the
    -- SMTP mailer generates for itself. UNIQUE both enforces one row per send and
    -- provides the webhook's only lookup index.
    provider_message_id VARCHAR(255) NOT NULL UNIQUE,
    recipient           VARCHAR(255) NOT NULL,
    kind                VARCHAR(30)  NOT NULL, -- 'welcome' | 'verify_email' | 'reset_password'
    status              VARCHAR(30)  NOT NULL, -- 'sent' | 'delivered' | 'delivery_delayed' | 'bounced' | 'complained'
    -- When the last applied provider event occurred, taken from the event payload
    -- rather than our clock: webhooks arrive out of order, and this is the only
    -- thing that can order them.
    last_event_at       TIMESTAMPTZ,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- For reading one user's recent mail history.
CREATE INDEX idx_usr_email_deliveries_user_id ON usr.email_deliveries(user_id, created_at DESC);

CREATE TABLE usr.email_suppressions (
    -- Stored lower-cased by every query that touches it; addresses differing only
    -- in case are the same mailbox, and two rows for one mailbox would mean the
    -- suppression check misses depending on how the address was typed.
    email      VARCHAR(255) PRIMARY KEY,
    reason     VARCHAR(30)  NOT NULL, -- 'bounced' | 'complained'
    detail     TEXT,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE usr.feed_outbox (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    event             JSONB       NOT NULL,
    attempts          INTEGER     NOT NULL DEFAULT 0,
    available_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error        TEXT,
    dead_lettered_at  TIMESTAMPTZ
);

CREATE INDEX idx_usr_feed_outbox_pending
    ON usr.feed_outbox (available_at, created_at)
    WHERE dead_lettered_at IS NULL;
