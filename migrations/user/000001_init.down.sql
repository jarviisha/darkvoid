-- Roll back the initial schema; all data in this module is removed.
DROP TABLE usr.feed_outbox;
DROP TABLE usr.email_suppressions;
DROP TABLE usr.email_deliveries;
DROP TABLE usr.email_tokens;
DROP TABLE usr.follows;
DROP TABLE usr.refresh_tokens;
DROP TABLE usr.user_roles;
DROP TABLE usr.users;
DROP FUNCTION usr.update_follow_counts();
DROP SCHEMA usr;
-- Shared extensions stay installed; other schemas may depend on them.
