-- Assignments naming 'bot' cannot be represented once the role is gone, and
-- leaving them would fail the narrowed CHECK below.
DELETE FROM usr.user_roles WHERE role = 'bot';

ALTER TABLE usr.user_roles DROP CONSTRAINT usr_user_roles_role_check;
ALTER TABLE usr.user_roles ADD CONSTRAINT usr_user_roles_role_check CHECK (role IN ('admin', 'moderator'));
