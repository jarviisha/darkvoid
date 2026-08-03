-- Feed knobs that used to be FEED_* environment variables, plus the three ranking
-- weights that were never configurable at all (they were literals in
-- feed.DefaultScorerConfig).
--
-- They are here for the same reason the bot's knobs are in bot.config: every one
-- of them is a thing an operator changes while watching a graph, and an
-- environment variable makes that a redeploy. A rollout percent in particular is
-- pointless if raising it costs a restart — the restart is the risk the staged
-- rollout exists to avoid.
--
-- What deliberately stayed in the environment: FEED_FANOUT_WORKERS and
-- FEED_FANOUT_QUEUE_SIZE. They size a goroutine pool and a channel at
-- construction, so a new value cannot take effect without rebuilding the
-- dispatcher — storing them here would offer a knob that silently does nothing
-- until the next restart, which is worse than one that honestly requires it.
--
-- Single-row table (CHECK id = 1), same as bot.config: a fixed typed set of
-- settings, with the ranges enforced by the database rather than re-derived in
-- every writer.
CREATE TABLE settings.feed (
    id                       SMALLINT         NOT NULL PRIMARY KEY DEFAULT 1 CHECK (id = 1),

    -- ── Prepared timeline serving ────────────────────────────────────────────
    -- Whether reads may be served from a precomputed timeline at all, and for what
    -- share of users. Kept as two columns rather than folding "off" into percent 0
    -- so an operator can park a rollout at 50% and flip serving off in one write
    -- without losing the number to go back to.
    timeline_enabled         BOOLEAN          NOT NULL DEFAULT FALSE,
    timeline_rollout_percent SMALLINT         NOT NULL DEFAULT 0
        CHECK (timeline_rollout_percent >= 0 AND timeline_rollout_percent <= 100),

    -- Entries retained per user timeline. The ceiling is a memory guard: every
    -- entry is a member of a Redis sorted set held for timeline_ttl_seconds, and
    -- the cost is per user, not global.
    timeline_max_items       INTEGER          NOT NULL DEFAULT 1000
        CHECK (timeline_max_items >= 1 AND timeline_max_items <= 10000),

    -- Seconds rather than INTERVAL, for the same reason as bot.config: the wire
    -- format is JSON and the consumer is a Go time.Duration.
    timeline_ttl_seconds     INTEGER          NOT NULL DEFAULT 604800
        CHECK (timeline_ttl_seconds > 0),

    -- Whether a timeline miss may rebuild that user's timeline inline. It is the
    -- knob to reach for first when the read path is slow under a cold cache: the
    -- rebuild happens on the request goroutine.
    timeline_refresh_on_miss BOOLEAN          NOT NULL DEFAULT TRUE,

    -- ── Fanout ───────────────────────────────────────────────────────────────
    -- Whether post/follow events fan out into prepared timelines. False leaves the
    -- dispatcher's workers running and idle, which is the point: it is a kill
    -- switch that can be undone without a restart.
    fanout_enabled           BOOLEAN          NOT NULL DEFAULT TRUE,

    -- Followers a single event will write to before giving up. Bounds the work one
    -- post by a very-followed account can put on the queue.
    fanout_max_followers     INTEGER          NOT NULL DEFAULT 10000
        CHECK (fanout_max_followers >= 1),

    -- ── Ranking weights ──────────────────────────────────────────────────────
    -- score = log(1+likes)*10 + recency_scale/(1+hours)^decay_exponent
    --         + relationship_bonus (when the viewer follows the author)
    --
    -- DOUBLE PRECISION rather than NUMERIC: these are read on every ranked request
    -- and fed straight into math.Pow, so the Go type is float64 either way and
    -- NUMERIC would only add a decode step. The upper bounds keep the three
    -- components on a comparable scale — a relationship_bonus of 10000 does not
    -- rank the feed, it just sorts followed authors first.
    relationship_bonus       DOUBLE PRECISION NOT NULL DEFAULT 10
        CHECK (relationship_bonus >= 0 AND relationship_bonus <= 1000),
    recency_scale            DOUBLE PRECISION NOT NULL DEFAULT 20
        CHECK (recency_scale >= 0 AND recency_scale <= 1000),
    -- Must be > 0: at 0 the recency term is the constant recency_scale for every
    -- post, which silently removes recency from the formula rather than flattening
    -- it. Operators reach for a small value (0.5) for that, not zero.
    decay_exponent           DOUBLE PRECISION NOT NULL DEFAULT 1.5
        CHECK (decay_exponent > 0 AND decay_exponent <= 10),

    -- usr.users(id) of the admin who last saved. No foreign key, same reason as
    -- bot.config.updated_by: this schema is not included in the user module's
    -- sqlc generation and never joins across schemas. NULL for the seeded row.
    updated_by               UUID,
    updated_at               TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

-- Seed the single row here rather than lazily on first read. The read path runs on
-- every settings refresh, and "no row yet" would be a second no-value case the
-- refresher has to tell apart from a failed query — the defaults above are the
-- answer in both cases, so there is no reason for the row not to exist.
INSERT INTO settings.feed (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
