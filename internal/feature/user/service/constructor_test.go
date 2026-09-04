package service

import (
	"strings"
	"testing"
)

// TestNewFollowService_NamesEveryMissingDependency replaces two separate ways
// this service could come up wrong.
//
// The constructor took a followRepo interface and then type-asserted it back to
// the concrete repository to recover WithTx. When that assertion failed and an
// outbox was wired, persistFollowMutation returned "feed outbox configured
// without transaction pool" — at request time, on the first follow, rather than
// at boot. Taking the concrete repository removes the assertion and with it that
// failure mode.
//
// Notifications is deliberately absent from the required set: the notification
// context reads the user repository, which SetupUserContext builds, so it cannot
// exist before the follow service does. That one arrives through
// WireNotificationEmitter.
func TestNewFollowService_NamesEveryMissingDependency(t *testing.T) {
	_, err := NewFollowService(FollowDeps{})

	if err == nil {
		t.Fatal("NewFollowService(FollowDeps{}) returned a nil error — want the missing dependencies named")
	}
	for _, want := range []string{"Repo", "Pool", "FeedInvalidator", "FeedOutbox"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name the missing %s dependency", err, want)
		}
	}
	if strings.Contains(err.Error(), "Notifications") {
		t.Errorf("error %q names Notifications — it is wired after construction because notification is built from the user repository", err)
	}
}
