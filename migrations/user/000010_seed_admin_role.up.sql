INSERT INTO usr.roles (name, description)
VALUES ('admin', 'Administrator')
ON CONFLICT (name) DO NOTHING;
