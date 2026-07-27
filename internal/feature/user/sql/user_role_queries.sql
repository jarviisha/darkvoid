-- User-Role Assignment Queries
--
-- Role names are stored inline on usr.user_roles and constrained by a CHECK;
-- there is no roles lookup table to join against.

-- name: AssignRoleToUser :exec
INSERT INTO usr.user_roles (
    user_id,
    role,
    assigned_by
) VALUES (
    $1, $2, $3
) ON CONFLICT (user_id, role) DO NOTHING;

-- name: RemoveRoleFromUser :exec
DELETE FROM usr.user_roles
WHERE user_id = $1 AND role = $2;

-- name: GetUserRoles :many
SELECT role, assigned_at, assigned_by
FROM usr.user_roles
WHERE user_id = $1
ORDER BY role;

-- name: CheckUserHasAnyRole :one
SELECT EXISTS (
    SELECT 1 FROM usr.user_roles
    WHERE user_id = $1 AND role = ANY(@roles::text[])
);
