package feed

import (
	"context"
	"math"
	"time"

	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
)

// RankedItem holds a post ID and its computed score.
type RankedItem struct {
	PostID string
	Score  float64
}

// Ranker scores a batch of posts and returns scores keyed by post ID.
// Implementations include the local formula-based LocalRanker.
type Ranker interface {
	// RankPosts scores the given posts and returns results keyed by post ID.
	// followingSet indicates which author IDs the viewer follows (for relationship bonus).
	RankPosts(ctx context.Context, posts []*feedentity.Post, followingSet map[string]bool, now time.Time) (map[string]float64, error)
}

// LocalRanker wraps the existing Scorer to implement the Ranker interface.
//
// It holds the settings rather than a ScorerConfig so a weight change reaches the
// read path on the next request. That is the whole reason the weights moved into
// the database: tuning a ranking formula is a loop of change-and-watch, and a
// redeploy per iteration makes the loop long enough that nobody runs it.
type LocalRanker struct {
	settings *Settings
}

// NewLocalRanker creates a LocalRanker reading its weights from settings.
func NewLocalRanker(settings *Settings) *LocalRanker {
	return &LocalRanker{settings: settings}
}

// RankPosts scores posts using the local formula.
//
// The weights are read once for the whole batch, not once per post: a settings
// change landing mid-batch would otherwise score the first half of a page on one
// formula and the second half on another, and the resulting order would be
// meaningless rather than merely stale.
func (r *LocalRanker) RankPosts(_ context.Context, posts []*feedentity.Post, followingSet map[string]bool, now time.Time) (map[string]float64, error) {
	cfg := r.settings.Get().Scorer
	scores := make(map[string]float64, len(posts))
	for _, p := range posts {
		hours := now.Sub(p.CreatedAt).Hours()
		if hours < 0 {
			hours = 0
		}

		engagement := math.Log1p(float64(p.LikeCount)) * 10
		recency := cfg.RecencyScale / math.Pow(1+hours, cfg.DecayExponent)

		bonus := 0.0
		if followingSet[p.AuthorID.String()] {
			bonus = cfg.RelationshipBonus
		}

		scores[p.ID.String()] = engagement + recency + bonus
	}
	return scores, nil
}
