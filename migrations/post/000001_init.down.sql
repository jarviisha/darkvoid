-- Roll back the initial schema; all data in this module is removed.
DROP TABLE post.comment_mentions;
DROP TABLE post.post_mentions;
DROP TABLE post.comment_likes;
DROP TABLE post.comment_media;
DROP TABLE post.post_hashtags;
DROP TABLE post.hashtags;
DROP TABLE post.comments;
DROP TABLE post.likes;
DROP TABLE post.post_media;
DROP TABLE post.posts;
DROP FUNCTION post.update_comment_like_count();
DROP FUNCTION post.update_comment_count();
DROP FUNCTION post.update_like_count();
DROP FUNCTION post.immutable_unaccent(text);
DROP SCHEMA post;
-- Shared extensions stay installed; other schemas may depend on them.
