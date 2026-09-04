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

// TestFollowServiceWires_RefuseNilAndSecondCall pins both deferred wires on the
// follow service. Each is a real cycle — the dispatcher needs posts, the
// notification context needs the user repository this service is built beside —
// and each is read by concurrent requests, so a second write is a data race.
func TestFollowServiceWires_RefuseNilAndSecondCall(t *testing.T) {
	t.Run("feed event emitter", func(t *testing.T) {
		svc := &FollowService{}
		if err := svc.WireFeedEventEmitter(nil); err == nil {
			t.Error("WireFeedEventEmitter(nil) returned a nil error — want a refusal")
		}
		if err := svc.WireFeedEventEmitter(&mockFollowFeedEmitter{}); err != nil {
			t.Fatalf("first WireFeedEventEmitter returned %v — want it accepted", err)
		}
		if err := svc.WireFeedEventEmitter(&mockFollowFeedEmitter{}); err == nil {
			t.Error("a second WireFeedEventEmitter returned a nil error — want a refusal")
		}
	})

	t.Run("notification emitter", func(t *testing.T) {
		svc := &FollowService{}
		if err := svc.WireNotificationEmitter(nil); err == nil {
			t.Error("WireNotificationEmitter(nil) returned a nil error — want a refusal")
		}
		if err := svc.WireNotificationEmitter(&mockNotifEmitter{}); err != nil {
			t.Fatalf("first WireNotificationEmitter returned %v — want it accepted", err)
		}
		if err := svc.WireNotificationEmitter(&mockNotifEmitter{}); err == nil {
			t.Error("a second WireNotificationEmitter returned a nil error — want a refusal")
		}
	})
}
