-- Exercise the behavior most easily lost when consolidating DDL.
DO $$
DECLARE
    author UUID;
    follower UUID;
    post_id UUID;
    comment_id UUID;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'bot') THEN
        RAISE EXCEPTION 'fresh installation created retired bot schema';
    END IF;
    IF (SELECT count(*) FROM settings.feed) <> 1 THEN
        RAISE EXCEPTION 'missing singleton feed settings';
    END IF;
    INSERT INTO usr.users (username, email, password_hash)
    VALUES ('baseline_author', 'author@example.test', 'test') RETURNING id INTO author;
    INSERT INTO usr.users (username, email, password_hash)
    VALUES ('baseline_follower', 'follower@example.test', 'test') RETURNING id INTO follower;
    INSERT INTO usr.user_roles (user_id, role) VALUES (author, 'admin'), (author, 'moderator'), (author, 'bot');
    BEGIN
        INSERT INTO usr.user_roles (user_id, role) VALUES (follower, 'unsupported');
        RAISE EXCEPTION 'unsupported role accepted';
    EXCEPTION WHEN check_violation THEN NULL;
    END;
    INSERT INTO usr.refresh_tokens (user_id, token_hash, expires_at)
    VALUES (author, repeat('a', 64), NOW() + INTERVAL '1 day');
    INSERT INTO usr.follows (follower_id, followee_id) VALUES (follower, author);
    IF (SELECT follower_count FROM usr.users WHERE id = author) <> 1
        OR (SELECT following_count FROM usr.users WHERE id = follower) <> 1 THEN
        RAISE EXCEPTION 'follow insert counters broken';
    END IF;
    DELETE FROM usr.follows WHERE follower_id = follower AND followee_id = author;
    IF (SELECT follower_count FROM usr.users WHERE id = author) <> 0
        OR (SELECT following_count FROM usr.users WHERE id = follower) <> 0 THEN
        RAISE EXCEPTION 'follow delete counters broken';
    END IF;
    INSERT INTO post.posts (author_id, content) VALUES (author, 'tiền ăn không') RETURNING id INTO post_id;
    IF NOT (SELECT search_vector @@ plainto_tsquery('simple', 'tien an khong') FROM post.posts WHERE id = post_id) THEN
        RAISE EXCEPTION 'Vietnamese search normalization broken';
    END IF;
    INSERT INTO post.likes (user_id, post_id) VALUES (follower, post_id);
    INSERT INTO post.comments (post_id, author_id, content) VALUES (post_id, follower, 'test') RETURNING id INTO comment_id;
    INSERT INTO post.comment_likes (user_id, comment_id) VALUES (author, comment_id);
    IF (SELECT like_count FROM post.posts WHERE id = post_id) <> 1
        OR (SELECT comment_count FROM post.posts WHERE id = post_id) <> 1
        OR (SELECT like_count FROM post.comments WHERE id = comment_id) <> 1 THEN
        RAISE EXCEPTION 'post/comment insert counters broken';
    END IF;
    DELETE FROM post.likes WHERE user_id = follower;
    DELETE FROM post.comment_likes WHERE user_id = author;
    UPDATE post.comments SET deleted_at = NOW() WHERE id = comment_id;
    IF (SELECT like_count FROM post.posts WHERE id = post_id) <> 0
        OR (SELECT comment_count FROM post.posts WHERE id = post_id) <> 0
        OR (SELECT like_count FROM post.comments WHERE id = comment_id) <> 0 THEN
        RAISE EXCEPTION 'post/comment removal counters broken';
    END IF;
    INSERT INTO usr.feed_outbox (event) VALUES ('{"type":"test"}');
    INSERT INTO notification.notifications (recipient_id, actor_id, type, group_key, message)
    VALUES (author, follower, 'follow', 'test', 'baseline notification');
END;
$$;
