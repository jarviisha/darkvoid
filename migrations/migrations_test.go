package migrations

import (
	"testing"
	"testing/fstest"
)

// TestLatestVersions_TakesTheHighestNumberedUpMigration pins the parse against a
// hand-built tree rather than the real one, so it does not have to be edited
// every time a migration is added — and so it can assert the case the real tree
// does not yet contain: 000010 sorts before 000002 lexically and after it
// numerically, and a health check that compared strings would call a fully
// migrated database behind.
func TestLatestVersions_TakesTheHighestNumberedUpMigration(t *testing.T) {
	fsys := fstest.MapFS{
		"post/000001_init.up.sql":           {},
		"post/000002_more.up.sql":           {},
		"post/000010_add_index.up.sql":      {},
		"post/000010_add_index.down.sql":    {},
		"user/000003_add_roles.up.sql":      {},
		"user/000003_add_roles.down.sql":    {},
		"settings/000002_feed_table.up.sql": {},
	}

	got, err := LatestVersions(fsys)
	if err != nil {
		t.Fatalf("LatestVersions: %v", err)
	}

	want := map[string]int{"post": 10, "user": 3, "settings": 2}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for module, version := range want {
		if got[module] != version {
			t.Errorf("%s = %d, want %d", module, got[module], version)
		}
	}
}
