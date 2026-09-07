-- Rebuild post.posts.search_vector on 'simple' + unaccent instead of 'english'.
--
-- The corpus is Vietnamese and Postgres ships no Vietnamese configuration, so
-- 'english' was never a choice between two right answers -- it was the template
-- default. It costs two things. Ordinary Vietnamese words that spell like
-- English stopwords are dropped from the index outright: to (big), an (eat),
-- do (because), no (it), so (compare), in (print) all index to an empty vector
-- under 'english'. And a query typed without diacritics -- which is how people
-- actually type Vietnamese -- matches nothing, because 'khong' and 'khong' are
-- different lexemes; the search returns an empty page that reads as "no
-- results" rather than as a broken search.
--
-- 'simple' keeps every token and does no stemming, which Vietnamese does not
-- need. unaccent folds the diacritics so both halves of the comparison land on
-- the same lexeme.

CREATE EXTENSION IF NOT EXISTS unaccent;

-- A generated column requires an IMMUTABLE expression and neither unaccent
-- overload is one: on PostgreSQL 16 pg_proc.provolatile is 's' for the 1-arg
-- and the 2-arg form alike, because the dictionary behind them can be replaced
-- without rewriting whatever depends on it.
--
-- This wrapper asserts an immutability the dictionary does not offer, and the
-- lie has a consequence worth stating: if unaccent.rules is ever edited, rows
-- already written keep the vector built under the old rules and nothing
-- rebuilds them. The repair is a reindex of the column, not a different
-- wrapper -- there is no formulation that both satisfies STORED and tracks a
-- mutable dictionary.
--
-- Both the function and the dictionary are addressed by schema, not left to
-- search_path: the expression is resolved once at column-definition time and a
-- caller's search_path must not be able to steer it.
CREATE OR REPLACE FUNCTION post.immutable_unaccent(text)
RETURNS text
LANGUAGE sql
IMMUTABLE STRICT PARALLEL SAFE
AS $$ SELECT public.unaccent('public.unaccent'::regdictionary, $1) $$;

-- Dropping the column takes idx_posts_search_vector with it, and adding the
-- generated column back rewrites post.posts under ACCESS EXCLUSIVE. That is the
-- real cost of this migration; there is no in-place way to change the
-- expression behind a STORED generated column.
ALTER TABLE post.posts DROP COLUMN IF EXISTS search_vector;

ALTER TABLE post.posts
    ADD COLUMN search_vector tsvector
        GENERATED ALWAYS AS (
            to_tsvector('simple', post.immutable_unaccent(coalesce(content, '')))
        ) STORED;

CREATE INDEX IF NOT EXISTS idx_posts_search_vector
    ON post.posts USING gin(search_vector)
    WHERE deleted_at IS NULL AND visibility = 'public';
