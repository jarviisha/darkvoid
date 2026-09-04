package app

import (
	"strings"
	"testing"
)

// TestPendingModules covers the four states a module can be in, because three of
// them are failures that look nothing alike in the database and identical from
// outside: behind, dirty, and never migrated at all.
func TestPendingModules(t *testing.T) {
	latest := map[string]int{"user": 12, "post": 7, "settings": 2}

	cases := []struct {
		name    string
		applied map[string]appliedMigration
		want    []string
	}{
		{
			name: "fully migrated",
			applied: map[string]appliedMigration{
				"user": {version: 12}, "post": {version: 7}, "settings": {version: 2},
			},
			want: nil,
		},
		{
			name: "one module behind",
			applied: map[string]appliedMigration{
				"user": {version: 12}, "post": {version: 6}, "settings": {version: 2},
			},
			want: []string{"post"},
		},
		{
			name: "never migrated at all — the table itself is absent",
			applied: map[string]appliedMigration{
				"user": {version: 12}, "post": {version: 7},
			},
			want: []string{"settings"},
		},
		{
			name: "at the right version but dirty",
			applied: map[string]appliedMigration{
				"user": {version: 12}, "post": {version: 7}, "settings": {version: 2, dirty: true},
			},
			want: []string{"settings"},
		},
		{
			// A database ahead of the binary is a rollback, not a broken deploy.
			// Migrations here are additive, so the older build still reads what it
			// knows about; unseating the instance would turn a deliberate rollback
			// into an outage.
			name: "ahead of this build",
			applied: map[string]appliedMigration{
				"user": {version: 13}, "post": {version: 7}, "settings": {version: 2},
			},
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pendingModules(latest, tc.applied)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("pendingModules = %v, want %v", got, tc.want)
			}
		})
	}
}
