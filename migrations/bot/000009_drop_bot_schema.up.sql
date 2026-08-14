-- Drop the bot schema: the content bot and its control plane have moved out of
-- this repository into a project of their own.
--
-- Nothing in this codebase reads bot.bots, bot.config, bot.topics or bot.runs any
-- more — the bounded context, the /api/v1/bot agent plane, the /admin/bots
-- operator surface and cmd/bot were all removed in the same change. What is left
-- behind without this migration is four populated tables no code can reach, which
-- a later reader has no way to tell from tables that are merely quiet.
--
-- The `bot` role in usr.user_roles deliberately stays (migrations/user/000012):
-- an account still needs to be markable as a machine account, and the external
-- bot project authenticates as an ordinary user holding it.
--
-- CASCADE because bot.runs references bot.bots. Nothing outside the schema
-- depends on it: every cross-schema id here is a plain UUID with no foreign key,
-- by design, so this drop cannot take a usr.* or post.* row with it.
--
-- The module stays in the Makefile's MIGRATION_MODULES until every environment
-- has run this. Removing migrations/bot in the same change would leave the
-- deployed databases holding an orphaned schema with nothing left in the repo to
-- clean it up.
--
-- Defense in depth: the production runner stops automatically at 000008, and
-- the explicit retirement runner sets this session parameter only after it has
-- validated manual approval, the external-bot handoff reference, and fresh
-- backup/restore-drill evidence. A direct or accidental `migrate up` therefore
-- fails before the destructive statement.
DO $migration_guard$
BEGIN
    IF current_setting('darkvoid.bot_schema_drop_approval', TRUE)
        IS DISTINCT FROM 'drop-bot-schema-000009' THEN
        RAISE EXCEPTION
            'bot schema retirement requires the approved destructive migration runner';
    END IF;
END
$migration_guard$;

DROP SCHEMA IF EXISTS bot CASCADE;
