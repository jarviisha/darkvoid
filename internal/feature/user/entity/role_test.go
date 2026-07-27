package entity

import "testing"

func TestParseRole_KnownRoles(t *testing.T) {
	for _, want := range AllRoles {
		got, ok := ParseRole(want.String())
		if !ok {
			t.Errorf("ParseRole(%q) should be recognised", want)
		}
		if got != want {
			t.Errorf("ParseRole(%q) = %q", want, got)
		}
	}
}

func TestParseRole_UnknownRoleRejected(t *testing.T) {
	for _, name := range []string{"", "Admin", "superuser", "admin "} {
		if _, ok := ParseRole(name); ok {
			t.Errorf("ParseRole(%q) should be rejected — it would fail the CHECK constraint", name)
		}
	}
}

// AllRoles drives both GET /admin/roles and the TotalRoles stat, so a role that
// exists as a constant but is missing from AllRoles would be silently
// unassignable through the API.
func TestAllRoles_MatchesKnownDescriptions(t *testing.T) {
	if len(AllRoles) != len(roleDescriptions) {
		t.Fatalf("AllRoles has %d entries but %d roles are described", len(AllRoles), len(roleDescriptions))
	}
	for _, r := range AllRoles {
		if r.Description() == "" {
			t.Errorf("role %q has no description", r)
		}
	}
}
