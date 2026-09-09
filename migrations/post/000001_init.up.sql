-- Initial schema baseline. Add future changes in a new numbered migration.

CREATE SCHEMA IF NOT EXISTS post;

CREATE EXTENSION IF NOT EXISTS unaccent;

-- STORED vectors depend on the standard unaccent dictionary. If its rules
-- change, rewrite the generated vectors before rebuilding their index.
CREATE OR REPLACE FUNCTION post.immutable_unaccent(text)
RETURNS text
LANGUAGE sql
IMMUTABLE STRICT PARALLEL SAFE
AS $$ SELECT public.unaccent('public.unaccent'::regdictionary, $1) $$;

CREATE TABLE post.posts (
    id         UUID        NOT NULL DEFAULT gen_random_uuid(),
    author_id  UUID        NOT NULL,
    content    TEXT        NOT NULL DEFAULT '',
    visibility VARCHAR(20) NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'followers', 'private')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    like_count BIGINT NOT NULL DEFAULT 0,
    comment_count BIGINT NOT NULL DEFAULT 0,
    search_vector tsvector GENERATED ALWAYS AS (
        to_tsvector('simple', post.immutable_unaccent(coalesce(content, '')))
    ) STORED,
    PRIMARY KEY (id)
);

CREATE INDEX idx_posts_author_id ON post.posts(author_id);
CREATE INDEX idx_posts_created_at ON post.posts(created_at DESC);
CREATE INDEX idx_posts_author_created ON post.posts(author_id, created_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE post.post_media (
    id         UUID        NOT NULL DEFAULT gen_random_uuid(),
    post_id    UUID        NOT NULL REFERENCES post.posts(id) ON DELETE CASCADE,
    media_key  TEXT        NOT NULL,
    media_type VARCHAR(20) NOT NULL CHECK (media_type IN ('image', 'video')),
    position   INT         NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);

CREATE INDEX idx_post_media_post_id ON post.post_media(post_id, position);

CREATE TABLE post.likes (
    user_id    UUID        NOT NULL,
    post_id    UUID        NOT NULL REFERENCES post.posts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, post_id)
);

CREATE INDEX idx_likes_post_id ON post.likes(post_id);

CREATE TABLE post.comments (
    id         UUID        NOT NULL DEFAULT gen_random_uuid(),
    post_id    UUID        NOT NULL REFERENCES post.posts(id) ON DELETE CASCADE,
    author_id  UUID        NOT NULL,
    parent_id  UUID        REFERENCES post.comments(id) ON DELETE CASCADE,
    content    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    like_count BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (id)
);

CREATE INDEX idx_comments_post_id ON post.comments(post_id, created_at ASC) WHERE deleted_at IS NULL;
CREATE INDEX idx_comments_parent_id ON post.comments(parent_id) WHERE parent_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX idx_comments_author_id ON post.comments(author_id);

CREATE OR REPLACE FUNCTION post.update_like_count()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE post.posts SET like_count = like_count + 1 WHERE id = NEW.post_id;
    ELSIF TG_OP = 'DELETE' THEN
        UPDATE post.posts SET like_count = GREATEST(like_count - 1, 0) WHERE id = OLD.post_id;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_like_count
AFTER INSERT OR DELETE ON post.likes
FOR EACH ROW EXECUTE FUNCTION post.update_like_count();

CREATE INDEX idx_posts_like_count ON post.posts(like_count DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_posts_trending ON post.posts(created_at DESC, like_count DESC) WHERE deleted_at IS NULL AND visibility = 'public';

-- Composite partial index for following feed cursor pagination.
-- Supports bitmap index scan on author_id = ANY($1::uuid[]) + row value
-- comparison (created_at, id) < (cursor_ts, cursor_id).
-- Partial (WHERE deleted_at IS NULL) keeps the index small by excluding soft-deleted rows.
CREATE INDEX idx_posts_author_created_id
    ON post.posts (author_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE OR REPLACE FUNCTION post.update_comment_count()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE post.posts SET comment_count = comment_count + 1 WHERE id = NEW.post_id;
    ELSIF TG_OP = 'UPDATE' AND OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
        UPDATE post.posts SET comment_count = GREATEST(comment_count - 1, 0) WHERE id = NEW.post_id;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_comment_count
AFTER INSERT OR UPDATE ON post.comments
FOR EACH ROW EXECUTE FUNCTION post.update_comment_count();

CREATE TABLE post.hashtags (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE post.post_hashtags (
    post_id    UUID        NOT NULL REFERENCES post.posts(id) ON DELETE CASCADE,
    hashtag_id UUID        NOT NULL REFERENCES post.hashtags(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (post_id, hashtag_id)
);

-- For trending query: COUNT per hashtag in a time window
CREATE INDEX idx_post_hashtags_hashtag_created
    ON post.post_hashtags (hashtag_id, created_at DESC);

CREATE TABLE post.comment_media (
    id         UUID        NOT NULL DEFAULT gen_random_uuid(),
    comment_id UUID        NOT NULL REFERENCES post.comments(id) ON DELETE CASCADE,
    media_key  TEXT        NOT NULL,
    media_type VARCHAR(20) NOT NULL CHECK (media_type IN ('image', 'video')),
    position   INT         NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);

CREATE INDEX idx_comment_media_comment_id ON post.comment_media(comment_id, position);

CREATE TABLE post.comment_likes (
    user_id    UUID        NOT NULL,
    comment_id UUID        NOT NULL REFERENCES post.comments(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, comment_id)
);

CREATE INDEX idx_comment_likes_comment_id ON post.comment_likes(comment_id);

-- Trigger to keep like_count in sync
CREATE OR REPLACE FUNCTION post.update_comment_like_count()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE post.comments SET like_count = like_count + 1 WHERE id = NEW.comment_id;
    ELSIF TG_OP = 'DELETE' THEN
        UPDATE post.comments SET like_count = GREATEST(like_count - 1, 0) WHERE id = OLD.comment_id;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_comment_like_count
AFTER INSERT OR DELETE ON post.comment_likes
FOR EACH ROW EXECUTE FUNCTION post.update_comment_like_count();

CREATE TABLE IF NOT EXISTS post.post_mentions (
    post_id    UUID        NOT NULL REFERENCES post.posts(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (post_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_post_mentions_post_id ON post.post_mentions(post_id);
CREATE INDEX IF NOT EXISTS idx_post_mentions_user_id ON post.post_mentions(user_id);

CREATE TABLE IF NOT EXISTS post.comment_mentions (
    comment_id UUID        NOT NULL REFERENCES post.comments(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (comment_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_comment_mentions_comment_id ON post.comment_mentions(comment_id);
CREATE INDEX IF NOT EXISTS idx_comment_mentions_user_id    ON post.comment_mentions(user_id);

CREATE INDEX idx_posts_search_vector
    ON post.posts USING gin(search_vector)
    WHERE deleted_at IS NULL AND visibility = 'public';
