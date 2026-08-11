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
