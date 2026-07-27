-- Collapse the usr.roles lookup table into a checked role name on usr.user_roles.
--
-- Roles are a fixed, application-defined set, not runtime data: the old design
-- let an admin POST an arbitrary role that no route ever checked. Storing the
-- name directly keeps the (assigned_by, assigned_at) audit trail and multi-role
-- support while removing a join, a UUID indirection, and the create-role API.
--
-- Adding a role from here on is a one-line migration (extend the CHECK) plus a
-- constant in internal/feature/user/entity/role.go.

ALTER TABLE usr.user_roles ADD COLUMN role VARCHAR(50);

UPDATE usr.user_roles ur
SET role = r.name
FROM usr.roles r
WHERE r.id = ur.role_id;

-- An assignment whose role row vanished cannot be represented; it was already
-- unusable because every permission check went through usr.roles.name.
DELETE FROM usr.user_roles WHERE role IS NULL;

-- Assignments naming a role outside the supported set would fail the CHECK
-- below. They could only have come from the removed POST /admin/roles endpoint.
DELETE FROM usr.user_roles WHERE role NOT IN ('admin', 'moderator');

-- DROP COLUMN role_id also drops idx_usr_user_roles_role_id.
-- The new primary key indexes (user_id, role), making idx_usr_user_roles_user_id
-- redundant on its leading column.
DROP INDEX IF EXISTS usr.idx_usr_user_roles_user_id;

ALTER TABLE usr.user_roles DROP CONSTRAINT user_roles_pkey;
ALTER TABLE usr.user_roles DROP COLUMN role_id;
ALTER TABLE usr.user_roles ALTER COLUMN role SET NOT NULL;
ALTER TABLE usr.user_roles ADD CONSTRAINT usr_user_roles_role_check CHECK (role IN ('admin', 'moderator'));
ALTER TABLE usr.user_roles ADD PRIMARY KEY (user_id, role);

CREATE INDEX idx_usr_user_roles_role ON usr.user_roles(role);

DROP TABLE usr.roles;
