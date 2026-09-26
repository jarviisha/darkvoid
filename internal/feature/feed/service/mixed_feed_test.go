package service

import (
	"math"
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

	// Trending order is score-descending, which is what the frontier walks.
	trendingWindow := []*feedentity.Post{highTrending, lowTrending}

	tests := []struct {
		name     string
		page     []*feedentity.FeedItem
		incoming *feed.FeedCursor
		window   recommendationWindow
		trending []*feedentity.Post
		assert   func(*testing.T, *feed.FeedCursor)
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
			trending: trendingWindow,
			assert: func(t *testing.T, cursor *feed.FeedCursor) {
				t.Helper()
				if cursor == nil || cursor.TrendingPostID != lowTrending.ID.String() || cursor.TrendingScore == nil || *cursor.TrendingScore != 4 {
					t.Fatalf("trending cursor = %#v, want score 4 and post %s", cursor, lowTrending.ID)
				}
			},
		},
		{
			// A trending post that is also a following candidate is collapsed to
			// SourceFollowing. The trending position must still advance past it,
			// or the next page re-serves it as trending.
			name: "trending advances past collapsed trending posts",
			page: []*feedentity.FeedItem{
				{Post: lowTrending, Source: feedentity.SourceFollowing},
				{Post: highTrending, Source: feedentity.SourceRecommendation},
			},
			trending: trendingWindow,
			assert: func(t *testing.T, cursor *feed.FeedCursor) {
				t.Helper()
				if cursor == nil || cursor.TrendingScore == nil || *cursor.TrendingScore != 4 || cursor.TrendingPostID != lowTrending.ID.String() {
					t.Fatalf("trending cursor = %#v, want score 4 and post %s", cursor, lowTrending.ID)
				}
			},
		},
		{
			name:     "collected but unshown trending starts from the top",
			page:     []*feedentity.FeedItem{{Post: olderFollowing, Source: feedentity.SourceFollowing}},
			trending: trendingWindow,
			assert: func(t *testing.T, cursor *feed.FeedCursor) {
				t.Helper()
				if cursor == nil || cursor.TrendingScore == nil || *cursor.TrendingScore != math.MaxFloat64 {
					t.Fatalf("trending cursor = %#v, want the not-started sentinel", cursor)
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
			transition := nextMixedCursor(userID, test.page, test.incoming, collectedSources{
				recWindow:      test.window,
				trendingWindow: test.trending,
			})
			cursor := transition.cursor
			test.assert(t, cursor)
		})
	}
}

// TestSourcesDry_Verdicts covers the exhaustion verdict directly. Reached only
// through GetFeed, a wrong answer here surfaces as a cursor that is nil or not for
// reasons several layers away.
func TestSourcesDry_Verdicts(t *testing.T) {
	post := &feedentity.Post{ID: uuid.New(), CreatedAt: time.Now().UTC()}
	followingPage := []*feedentity.FeedItem{{Post: post, Source: feedentity.SourceFollowing}}

	tests := []struct {
		name               string
		page               []*feedentity.FeedItem
		sources            collectedSources
		trendingConsumed   bool
		trendingSeen       []string
		recommendationsDry bool
		want               bool
	}{
		{
			name:               "everything spent",
			page:               followingPage,
			sources:            collectedSources{followingCount: 1},
			trendingConsumed:   true,
			recommendationsDry: true,
			want:               true,
		},
		{
			name:               "following fetch hit its limit",
			page:               followingPage,
			sources:            collectedSources{followingCount: 1, followingTruncated: true},
			trendingConsumed:   true,
			recommendationsDry: true,
		},
		{
			name:               "trending window hit its limit",
			page:               followingPage,
			sources:            collectedSources{followingCount: 1, trendingTruncated: true},
			trendingConsumed:   true,
			recommendationsDry: true,
		},
		{
			name:               "a source errored",
			page:               followingPage,
			sources:            collectedSources{followingCount: 1, sourceFailed: true},
			trendingConsumed:   true,
			recommendationsDry: true,
		},
		{
			name:               "recommendations still have pages",
			page:               followingPage,
			sources:            collectedSources{followingCount: 1},
			trendingConsumed:   true,
			recommendationsDry: false,
		},
		{
			name:               "trending window not fully served",
			page:               followingPage,
			sources:            collectedSources{followingCount: 1},
			trendingConsumed:   false,
			recommendationsDry: true,
		},
		{
			name:               "trending carries a ragged edge",
			page:               followingPage,
			sources:            collectedSources{followingCount: 1},
			trendingConsumed:   true,
			trendingSeen:       []string{uuid.NewString()},
			recommendationsDry: true,
		},
		{
			name:               "following fetched more rows than the page served",
			page:               followingPage,
			sources:            collectedSources{followingCount: 5},
			trendingConsumed:   true,
			recommendationsDry: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sourcesDry(test.page, test.sources, test.trendingConsumed, test.trendingSeen, test.recommendationsDry)
			if got != test.want {
				t.Fatalf("sourcesDry() = %v, want %v", got, test.want)
			}
		})
	}
}

// TestCarryDiscoverSeen_DropsIDsTheBoundaryHasPassed pins the re-filter. The
// following position descends as the scroll goes, so an id that discover could
// reach on one page is above the handoff point later and only occupies a cap slot.
func TestCarryDiscoverSeen_DropsIDsTheBoundaryHasPassed(t *testing.T) {
	now := time.Now().UTC()
	old := feed.SeenPost{CreatedAt: now.Add(-72 * time.Hour), PostID: uuid.NewString()}
	deepBoundary := now.Add(-120 * time.Hour).UnixNano()
	next := &feed.FeedCursor{FollowingCreatedAt: &deepBoundary, FollowingPostID: uuid.NewString()}
	incoming := &feed.FeedCursor{DiscoverSeen: []string{old.Encode()}}

	if got := carryDiscoverSeen(nil, incoming, next); got != nil {
		t.Fatalf("carryDiscoverSeen() = %v, want the unreachable id dropped", got)
	}

	shallowBoundary := now.Add(-time.Hour).UnixNano()
	reachable := &feed.FeedCursor{FollowingCreatedAt: &shallowBoundary, FollowingPostID: uuid.NewString()}
	if got := carryDiscoverSeen(nil, incoming, reachable); len(got) != 1 {
		t.Fatalf("carryDiscoverSeen() = %v, want the reachable id kept", got)
	}
}

// TestSeenCapsHoldTwoPagesOfRaggedEdge pins the caps against the page size they
// have to absorb. The caps live in package feed and the page size in this one, so
// nothing but this test fails if one moves: a cap below what a page can add turns
// into posts served twice, silently.
func TestSeenCapsHoldTwoPagesOfRaggedEdge(t *testing.T) {
	if feed.MaxTrendingSeen < pageSize*2 {
		t.Fatalf("MaxTrendingSeen = %d, want at least two pages (%d)", feed.MaxTrendingSeen, pageSize*2)
	}
	if feed.MaxDiscoverSeen < pageSize {
		t.Fatalf("MaxDiscoverSeen = %d, want at least one page (%d)", feed.MaxDiscoverSeen, pageSize)
	}
}
