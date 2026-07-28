-- Add the 'bot' role, required by the /api/v1/bot/* agent plane.
--
-- The content bot (cmd/bot) authenticates as an ordinary user and polls the API
-- for its desired state. Guarding those routes with RequireRole(..., 'bot')
-- reuses the existing JWT + role machinery instead of introducing a second
-- authentication scheme just for machine callers.
--
-- Keep in sync with internal/feature/user/entity/role.go — a role must exist in
-- both the CHECK constraint and the constant set, per 000011.

ALTER TABLE usr.user_roles DROP CONSTRAINT usr_user_roles_role_check;
ALTER TABLE usr.user_roles ADD CONSTRAINT usr_user_roles_role_check CHECK (role IN ('admin', 'moderator', 'bot'));
