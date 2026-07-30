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
