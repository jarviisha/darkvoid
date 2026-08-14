package service

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// mixedFeedBuilder collects following, recommendation, and trending sources,
// then ranks and orders their candidates. Cursor transition remains a pure
// function so pagination invariants can be tested without provider I/O.
type mixedFeedBuilder struct {
	postReader  feed.PostReader
	ranker      feed.Ranker
	recommender feed.Recommender
	trending    *trendingSource
}

type feedCandidate struct {
	post                 *feedentity.Post
	source               feedentity.Source
	sourceRank           int
	providerScore        *float64
	providerRank         *int
	recommendationScore  *float64
	recommendationRank   *int
	recommendationOffset *int
}

type recommendationWindow struct {
	start        int
	end          int
	total        int
	validOffsets map[int]bool
}

func recommendationOffset(cursor *feed.FeedCursor) int {
	if cursor == nil {
		return 0
	}
	return cursor.RecommendationOffset
}

func recommendationSeenSet(cursor *feed.FeedCursor) map[int]bool {
	seen := make(map[int]bool)
	if cursor == nil {
		return seen
	}
	for _, offset := range cursor.RecommendationSeen {
		seen[offset] = true
	}
	return seen
}

func recommendationCandidateOffsets(candidates []feedCandidate) map[int]bool {
	offsets := make(map[int]bool)
	for _, candidate := range candidates {
		if candidate.recommendationOffset != nil {
			offsets[*candidate.recommendationOffset] = true
		}
	}
	return offsets
}

func (b *mixedFeedBuilder) collect(ctx context.Context, userID uuid.UUID, authorIDs []uuid.UUID, cursor *feed.FeedCursor) ([]feedCandidate, recommendationWindow, bool, bool, error) {
	offset := recommendationOffset(cursor)
	recWindow := recommendationWindow{start: offset, end: offset}
	candidates := make([]feedCandidate, 0, pageSize*fetchMultiplier)

	followingPosts, err := b.postReader.GetFollowingPostsWithCursor(ctx, authorIDs, userID, cursor.FollowingPosition(), pageSize*fetchMultiplier)
	if err != nil {
		logger.LogError(ctx, err, "failed to get following posts", "user_id", userID)
		return nil, recWindow, false, false, errors.NewInternalError(err)
	}
	for i, post := range followingPosts {
		candidates = append(candidates, feedCandidate{post: post, source: feedentity.SourceFollowing, sourceRank: i + 1})
	}

	if b.recommender != nil {
		recommendations, recommendationErr := b.recommender.GetRecommendations(ctx, userID.String(), pageSize, offset)
		if recommendationErr != nil {
			logger.LogError(ctx, recommendationErr, "codohue recommendations failed, skipping", "user_id", userID)
		} else if recommendations != nil {
			recWindow.start = recommendations.Offset
			recWindow.end = recommendations.Offset + len(recommendations.Items)
			recWindow.total = recommendations.Total
			recommendationCandidates, loadErr := b.loadRecommendations(ctx, recommendations.Items, recommendations.Offset, cursor)
			if loadErr != nil {
				logger.LogError(ctx, loadErr, "failed to load recommendation candidates", "user_id", userID)
			}
			candidates = append(candidates, recommendationCandidates...)
		}
	}

	trendingCollected := false
	if cursor == nil || cursor.TrendingPosition() != nil {
		trendingPosts, trendingErr := b.trending.get(ctx)
		if trendingErr != nil {
			logger.LogError(ctx, trendingErr, "failed to get trending posts, skipping", "user_id", userID)
		}
		trendingPosts = applyTrendingCursor(trendingPosts, cursor.TrendingPosition(), pageSize)
		for i, post := range trendingPosts {
			candidates = append(candidates, feedCandidate{post: post, source: feedentity.SourceTrending, sourceRank: i + 1})
		}
		trendingCollected = len(trendingPosts) > 0
	}

	return candidates, recWindow, trendingCollected, len(followingPosts) > 0, nil
}

func (b *mixedFeedBuilder) loadRecommendations(ctx context.Context, items []feed.RecommendedItem, baseOffset int, cursor *feed.FeedCursor) ([]feedCandidate, error) {
	ids := make([]uuid.UUID, 0, len(items))
	metadata := make(map[uuid.UUID]feed.RecommendedItem, len(items))
	offsets := make(map[uuid.UUID]int, len(items))
	seen := recommendationSeenSet(cursor)
	for i, item := range items {
		offset := baseOffset + i
		if seen[offset] || item.Score < 0 {
			continue
		}
		id, err := uuid.Parse(item.ObjectID)
		if err != nil {
			continue
		}
		ids = append(ids, id)
		metadata[id] = item
		offsets[id] = offset
	}
	posts, err := b.postReader.GetPostsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	result := make([]feedCandidate, 0, len(posts))
	for _, post := range posts {
		if post.Visibility != "public" {
			continue
		}
		item := metadata[post.ID]
		score := item.Score
		rank := item.Rank
		offset := offsets[post.ID]
		result = append(result, feedCandidate{
			post:                 post,
			source:               feedentity.SourceRecommendation,
			sourceRank:           item.Rank,
			providerScore:        &score,
			providerRank:         &rank,
			recommendationScore:  &score,
			recommendationRank:   &rank,
			recommendationOffset: &offset,
		})
	}
	return result, nil
}

func collapseCandidates(candidates []feedCandidate) []feedCandidate {
	byID := make(map[uuid.UUID]feedCandidate, len(candidates))
	for _, candidate := range candidates {
		if candidate.post == nil {
			continue
		}
		existing, ok := byID[candidate.post.ID]
		if !ok || sourcePriority(candidate.source) > sourcePriority(existing.source) {
			byID[candidate.post.ID] = candidate
			continue
		}
		if candidate.providerScore != nil && existing.providerScore == nil {
			existing.providerScore = candidate.providerScore
			existing.providerRank = candidate.providerRank
			existing.recommendationScore = candidate.recommendationScore
			existing.recommendationRank = candidate.recommendationRank
			existing.recommendationOffset = candidate.recommendationOffset
			byID[candidate.post.ID] = existing
		}
	}
	result := make([]feedCandidate, 0, len(byID))
	for _, candidate := range byID {
		result = append(result, candidate)
	}
	return result
}

func filterEligibleCandidates(userID uuid.UUID, followingSet map[uuid.UUID]bool, candidates []feedCandidate) []feedCandidate {
	filtered := candidates[:0]
	for _, candidate := range candidates {
		if candidate.post == nil {
			continue
		}
		switch candidate.source {
		case feedentity.SourceFollowing:
			if isEligibleTimelinePost(userID, followingSet, candidate.post) {
				filtered = append(filtered, candidate)
			}
		case feedentity.SourceRecommendation:
			if candidate.post.Visibility == "public" && candidate.post.AuthorID != userID {
				filtered = append(filtered, candidate)
			}
		case feedentity.SourceTrending, feedentity.SourceDiscover:
			if candidate.post.Visibility == "public" {
				filtered = append(filtered, candidate)
			}
		default:
			if candidate.post.Visibility == "public" {
				filtered = append(filtered, candidate)
			}
		}
	}
	return filtered
}

func sourcePriority(source feedentity.Source) int {
	switch source {
	case feedentity.SourceFollowing:
		return 4
	case feedentity.SourceRecommendation:
		return 3
	case feedentity.SourceTrending:
		return 2
	default:
		return 1
	}
}

func (b *mixedFeedBuilder) rank(ctx context.Context, candidates []feedCandidate, followingSet map[uuid.UUID]bool, now time.Time) []*feedentity.FeedItem {
	posts := make([]*feedentity.Post, len(candidates))
	for i, candidate := range candidates {
		posts[i] = candidate.post
	}
	followingStrings := make(map[string]bool, len(followingSet))
	for id := range followingSet {
		followingStrings[id.String()] = true
	}
	scores, err := b.ranker.RankPosts(ctx, posts, followingStrings, now)
	if err != nil {
		logger.LogError(ctx, err, "ranker failed, falling back to chronological order")
		scores = make(map[string]float64)
	}
	items := make([]*feedentity.FeedItem, 0, len(candidates))
	for _, candidate := range candidates {
		score := scores[candidate.post.ID.String()]
		if candidate.providerScore != nil {
			score += *candidate.providerScore * 20
		}
		if candidate.providerRank != nil && *candidate.providerRank > 0 {
			score += 5 / float64(*candidate.providerRank)
		}
		items = append(items, &feedentity.FeedItem{
			Post:                 candidate.post,
			Score:                score,
			Source:               candidate.source,
			RecommendationScore:  candidate.recommendationScore,
			RecommendationRank:   candidate.recommendationRank,
			RecommendationOffset: candidate.recommendationOffset,
		})
	}
	return items
}

func (b *mixedFeedBuilder) sort(items []*feedentity.FeedItem) {
	sort.Slice(items, func(i, j int) bool {
		if math.Abs(items[i].Score-items[j].Score) > scoreEpsilon {
			return items[i].Score > items[j].Score
		}
		if !items[i].Post.CreatedAt.Equal(items[j].Post.CreatedAt) {
			return items[i].Post.CreatedAt.After(items[j].Post.CreatedAt)
		}
		return items[i].Post.ID.String() > items[j].Post.ID.String()
	})
}

func nextMixedCursor(userID uuid.UUID, page []*feedentity.FeedItem, incoming *feed.FeedCursor, recWindow recommendationWindow, trendingCollected bool) *feed.FeedCursor {
	next := &feed.FeedCursor{TimelineUser: userID.String()}
	seen := recommendationSeenSet(incoming)
	for _, item := range page {
		if item.RecommendationOffset != nil {
			seen[*item.RecommendationOffset] = true
		}
	}
	recommendationOffset := recWindow.start
	for recommendationOffset < recWindow.end && (seen[recommendationOffset] || !recWindow.validOffsets[recommendationOffset]) {
		delete(seen, recommendationOffset)
		recommendationOffset++
	}
	if recWindow.total > 0 && recommendationOffset < recWindow.total {
		next.RecommendationOffset = recommendationOffset
		for offset := range seen {
			if offset >= recommendationOffset {
				next.RecommendationSeen = append(next.RecommendationSeen, offset)
			}
		}
		sort.Ints(next.RecommendationSeen)
	}

	if lowest := lowestTrendingShown(page); lowest != nil {
		score := trendScoreFromPost(lowest)
		next.TrendingScore = &score
		next.TrendingPostID = lowest.ID.String()
	} else if incoming.TrendingPosition() != nil {
		next.TrendingScore = incoming.TrendingScore
		next.TrendingPostID = incoming.TrendingPostID
	} else if trendingCollected {
		score := math.MaxFloat64
		next.TrendingScore = &score
		next.TrendingPostID = uuid.Max.String()
	}

	if oldest := oldestFollowingShown(page); oldest != nil {
		timestamp := oldest.CreatedAt.UnixNano()
		next.FollowingCreatedAt = &timestamp
		next.FollowingPostID = oldest.ID.String()
	} else if incoming != nil && incoming.FollowingCreatedAt != nil {
		next.FollowingCreatedAt = incoming.FollowingCreatedAt
		next.FollowingPostID = incoming.FollowingPostID
	}
	if !next.HasContinuation() {
		return nil
	}
	return next
}

func lowestTrendingShown(page []*feedentity.FeedItem) *feedentity.Post {
	var lowest *feedentity.Post
	for _, item := range page {
		if item.Source != feedentity.SourceTrending || item.Post == nil {
			continue
		}
		post := item.Post
		if lowest == nil {
			lowest = post
			continue
		}
		postScore, lowestScore := trendScoreFromPost(post), trendScoreFromPost(lowest)
		if postScore < lowestScore || (postScore == lowestScore && post.ID.String() < lowest.ID.String()) {
			lowest = post
		}
	}
	return lowest
}

func oldestFollowingShown(page []*feedentity.FeedItem) *feedentity.Post {
	var oldest *feedentity.Post
	for _, item := range page {
		if item.Source != feedentity.SourceFollowing || item.Post == nil {
			continue
		}
		post := item.Post
		if oldest == nil || post.CreatedAt.Before(oldest.CreatedAt) ||
			(post.CreatedAt.Equal(oldest.CreatedAt) && post.ID.String() < oldest.ID.String()) {
			oldest = post
		}
	}
	return oldest
}
