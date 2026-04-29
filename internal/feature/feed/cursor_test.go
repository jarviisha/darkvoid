package feed

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFeedCursor_RoundTrip(t *testing.T) {
	now := time.Now().UTC()
	state := NewFeedPageState(now)
	state.FollowingCursor = &FollowingCursor{Mode: ModeFollowing, CreatedAt: now, PostID: uuid.New().String()}
	state.RecommendationOffset = 20
	state.RecommendationTotal = 100
	state.AddSeen(uuid.New().String())

	encoded := (&FeedCursor{State: state}).Encode()
	decoded, err := DecodeFeedCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeFeedCursor: %v", err)
	}
	if decoded.State.RecommendationOffset != 20 || len(decoded.State.SeenPostIDs) != 1 {
		t.Fatalf("decoded state mismatch: %+v", decoded.State)
	}
}

func TestFeedCursor_RejectsExpired(t *testing.T) {
	state := NewFeedPageState(time.Now().UTC().Add(-FeedSessionTTL * 2))
	state.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	encoded := (&FeedCursor{State: state}).Encode()
	if _, err := DecodeFeedCursor(encoded); err == nil {
		t.Fatal("expected expired cursor error")
	}
}

func TestFeedPageState_AddSeenCapsIDs(t *testing.T) {
	state := NewFeedPageState(time.Now().UTC())
	for i := 0; i < MaxFeedSeenIDs+10; i++ {
		state.AddSeen(uuid.New().String())
	}
	if len(state.SeenPostIDs) != MaxFeedSeenIDs {
		t.Fatalf("seen IDs len = %d, want %d", len(state.SeenPostIDs), MaxFeedSeenIDs)
	}
}
