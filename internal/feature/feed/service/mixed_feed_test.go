package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
)

func TestNextMixedCursor_SourceTransitions(t *testing.T) {
	userID := uuid.New()
	now := time.Now().UTC()
	olderFollowing := &feedentity.Post{ID: uuid.New(), CreatedAt: now.Add(-time.Hour)}
	newerFollowing := &feedentity.Post{ID: uuid.New(), CreatedAt: now}
	lowTrending := &feedentity.Post{ID: uuid.New(), LikeCount: 4}
	highTrending := &feedentity.Post{ID: uuid.New(), LikeCount: 9}
	carriedTrendScore := 3.0
	carriedFollowingTime := now.Add(-2 * time.Hour).UnixNano()
	carriedTrendID := uuid.New().String()
	carriedFollowingID := uuid.New().String()
	recommendationOffset := 0

	tests := []struct {
		name              string
		page              []*feedentity.FeedItem
		incoming          *feed.FeedCursor
		window            recommendationWindow
		trendingCollected bool
		assert            func(*testing.T, *feed.FeedCursor)
	}{
		{
			name: "no continuation",
			assert: func(t *testing.T, cursor *feed.FeedCursor) {
				t.Helper()
				if cursor != nil {
					t.Fatalf("cursor = %#v, want nil", cursor)
				}
			},
		},
		{
			name: "following advances to oldest item shown",
			page: []*feedentity.FeedItem{
				{Post: olderFollowing, Source: feedentity.SourceFollowing},
				{Post: newerFollowing, Source: feedentity.SourceFollowing},
			},
			assert: func(t *testing.T, cursor *feed.FeedCursor) {
				t.Helper()
				if cursor == nil || cursor.FollowingPostID != olderFollowing.ID.String() {
					t.Fatalf("following cursor = %#v, want post %s", cursor, olderFollowing.ID)
				}
			},
		},
		{
			name: "trending advances to lowest score shown",
			page: []*feedentity.FeedItem{
				{Post: lowTrending, Source: feedentity.SourceTrending},
				{Post: highTrending, Source: feedentity.SourceTrending},
			},
			assert: func(t *testing.T, cursor *feed.FeedCursor) {
				t.Helper()
				if cursor == nil || cursor.TrendingPostID != lowTrending.ID.String() || cursor.TrendingScore == nil || *cursor.TrendingScore != 4 {
					t.Fatalf("trending cursor = %#v, want score 4 and post %s", cursor, lowTrending.ID)
				}
			},
		},
		{
			name: "unshown sources retain incoming positions",
			incoming: &feed.FeedCursor{
				TrendingScore:      &carriedTrendScore,
				TrendingPostID:     carriedTrendID,
				FollowingCreatedAt: &carriedFollowingTime,
				FollowingPostID:    carriedFollowingID,
			},
			assert: func(t *testing.T, cursor *feed.FeedCursor) {
				t.Helper()
				if cursor == nil || cursor.TrendingPostID != carriedTrendID || cursor.FollowingPostID != carriedFollowingID {
					t.Fatalf("cursor = %#v, want incoming source positions", cursor)
				}
			},
		},
		{
			name: "recommendation skips shown and invalid offsets",
			page: []*feedentity.FeedItem{{
				Post:                 &feedentity.Post{ID: uuid.New()},
				Source:               feedentity.SourceRecommendation,
				RecommendationOffset: &recommendationOffset,
			}},
			window: recommendationWindow{
				start:        0,
				end:          3,
				total:        4,
				validOffsets: map[int]bool{0: true, 2: true},
			},
			assert: func(t *testing.T, cursor *feed.FeedCursor) {
				t.Helper()
				if cursor == nil || cursor.RecommendationOffset != 2 {
					t.Fatalf("recommendation cursor = %#v, want offset 2", cursor)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cursor := nextMixedCursor(userID, test.page, test.incoming, test.window, test.trendingCollected)
			test.assert(t, cursor)
		})
	}
}
