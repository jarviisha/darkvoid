-- Restore the usr.roles lookup table and the role_id foreign key.

CREATE TABLE usr.roles (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(50)  UNIQUE NOT NULL,
    description TEXT,
    created_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP
);

-- Recreate the seed row from 000010 plus any other role still referenced.
INSERT INTO usr.roles (name, description)
VALUES ('admin', 'Administrator')
ON CONFLICT (name) DO NOTHING;

INSERT INTO usr.roles (name)
SELECT DISTINCT role FROM usr.user_roles
ON CONFLICT (name) DO NOTHING;

ALTER TABLE usr.user_roles ADD COLUMN role_id UUID REFERENCES usr.roles(id) ON DELETE CASCADE;

UPDATE usr.user_roles ur
SET role_id = r.id
FROM usr.roles r
WHERE r.name = ur.role;

DROP INDEX IF EXISTS usr.idx_usr_user_roles_role;

ALTER TABLE usr.user_roles DROP CONSTRAINT user_roles_pkey;
ALTER TABLE usr.user_roles DROP CONSTRAINT usr_user_roles_role_check;
ALTER TABLE usr.user_roles DROP COLUMN role;
ALTER TABLE usr.user_roles ALTER COLUMN role_id SET NOT NULL;
ALTER TABLE usr.user_roles ADD PRIMARY KEY (user_id, role_id);

CREATE INDEX idx_usr_user_roles_user_id ON usr.user_roles(user_id);
CREATE INDEX idx_usr_user_roles_role_id ON usr.user_roles(role_id);
