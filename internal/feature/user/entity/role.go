package entity

import (
	"time"

	"github.com/google/uuid"
)

// Role is a system role name. The set of roles is fixed by the application:
// it is defined by the constants below and enforced by the CHECK constraint on
// usr.user_roles.role. Roles are not runtime data, so there is no API to create
// one — adding a role means extending both in the same change.
type Role string

const (
	// RoleAdmin grants access to every /api/v1/admin route and the admin Swagger UI.
	RoleAdmin Role = "admin"
	// RoleModerator is reserved for content moderation. It can be assigned, but no
	// route enforces it yet.
	RoleModerator Role = "moderator"
)

// roleDescriptions is the authoritative set of known roles. Keep it in sync with
// the CHECK constraint in migrations/user/000011_inline_role_names.up.sql.
var roleDescriptions = map[Role]string{
	RoleAdmin:     "Full access to the admin API",
	RoleModerator: "Reserved for content moderation",
}

// AllRoles lists every role the system recognises, ordered by name.
var AllRoles = []Role{RoleAdmin, RoleModerator}

// ParseRole converts a raw string into a Role, reporting whether it names a role
// the system recognises. Unknown names must be rejected before they reach the DB,
// where they would fail the CHECK constraint as an opaque error.
func ParseRole(s string) (Role, bool) {
	r := Role(s)
	_, ok := roleDescriptions[r]
	return r, ok
}

// String returns the role name exactly as stored in the database.
func (r Role) String() string { return string(r) }

// Description returns human-readable text for the role, or "" if unknown.
func (r Role) Description() string { return roleDescriptions[r] }

// RoleAssignment records that a user holds a role, along with the audit trail of
// who granted it and when. AssignedBy is nil for grants made by the system itself
// — the root bootstrap on boot, or darkvoidctl.
type RoleAssignment struct {
	Role       Role
	AssignedAt time.Time
	AssignedBy *uuid.UUID
}
