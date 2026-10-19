-- Records the order the author wrote mentions in. created_at cannot: every
-- mention of one post is inserted in the post's transaction, where NOW() is
-- fixed. Existing rows take position 0 and stay unordered among themselves.
ALTER TABLE post.post_mentions ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
ALTER TABLE post.comment_mentions ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
