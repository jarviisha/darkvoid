-- Restore the 'english' generated column from 000014. This rewrites
-- post.posts again, and re-introduces the two failures described in the up
-- migration; it exists so the pair is reversible, not because rolling back is
-- a state anyone should sit in.
--
-- Rolling back also means the query side has to go back with it. Both halves
-- name their text search configuration, and a search built with 'simple' +
-- unaccent against a column generated with 'english' matches nothing at all
-- rather than matching worse.

DROP INDEX IF EXISTS post.idx_posts_search_vector;
ALTER TABLE post.posts DROP COLUMN IF EXISTS search_vector;

ALTER TABLE post.posts
    ADD COLUMN search_vector tsvector
        GENERATED ALWAYS AS (to_tsvector('english', coalesce(content, ''))) STORED;

CREATE INDEX IF NOT EXISTS idx_posts_search_vector
    ON post.posts USING gin(search_vector)
    WHERE deleted_at IS NULL AND visibility = 'public';

DROP FUNCTION IF EXISTS post.immutable_unaccent(text);

-- The extension stays installed. It is database-wide rather than owned by this
-- module, dropping it would cascade into anything else that has since started
-- using it, and an unused extension costs nothing.
