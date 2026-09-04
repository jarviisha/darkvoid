package service

import (
	"strings"
	"testing"
)

// TestNewAdminService_NamesEveryMissingDependency closes the last cross-context
// setter that could be forgotten silently. SetupAdminContext already runs after
// the notification context, so the emitter never needed to arrive by mutation —
// it only did because every other context did it that way.
//
// Storage joins the required set for a plainer reason: ListUsers resolves avatar
// URLs through it, so a nil one is a panic waiting for the first admin listing,
// not a disabled feature.
func TestNewAdminService_NamesEveryMissingDependency(t *testing.T) {
	_, err := NewAdminService(AdminDeps{})

	if err == nil {
		t.Fatal("NewAdminService(AdminDeps{}) returned a nil error — want the missing dependencies named")
	}
	for _, want := range []string{"Users", "Roles", "Storage", "Notifications"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name the missing %s dependency", err, want)
		}
	}
}
