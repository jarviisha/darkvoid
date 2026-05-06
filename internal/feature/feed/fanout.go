package feed

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// FanoutWorker writes post-created events into followers' prepared timelines.
type FanoutWorker struct {
	followerReader FollowerReader
	timeline       TimelineStore
	maxFollowers   int
}

func NewFanoutWorker(followerReader FollowerReader, timeline TimelineStore, maxFollowers int) *FanoutWorker {
	if maxFollowers <= 0 {
		maxFollowers = 10000
	}
	return &FanoutWorker{
		followerReader: followerReader,
		timeline:       timeline,
		maxFollowers:   maxFollowers,
	}
}

func (w *FanoutWorker) HandleFeedEvent(ctx context.Context, event Event) error {
	if w == nil {
		return nil
	}
	switch event.Type {
	case EventPostCreated:
		return w.handlePostCreated(ctx, event)
	default:
		return nil
	}
}

func (w *FanoutWorker) handlePostCreated(ctx context.Context, event Event) error {
	start := time.Now()
	followers, err := w.followerReader.GetFollowerIDs(ctx, event.AuthorID)
	if err != nil {
		CountFanoutError()
		return fmt.Errorf("get follower IDs: %w", err)
	}
	originalFollowerCount := len(followers)
	if len(followers) > w.maxFollowers {
		followers = followers[:w.maxFollowers]
		CountFanoutCapped()
		logger.Info(ctx, "fanout follower list capped", "post_id", event.PostID, "author_id", event.AuthorID, "followers", originalFollowerCount, "cap", w.maxFollowers)
	}
	entry := TimelineEntry{PostID: event.PostID, Score: event.Score}
	var attempted, succeeded, failed int
	var lastErr error
	for _, followerID := range followers {
		if followerID == uuid.Nil {
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			lastErr = ctxErr
			break
		}
		attempted++
		if err := w.timeline.AddPost(ctx, followerID, entry); err != nil {
			CountFanoutError()
			failed++
			lastErr = err
			logger.LogError(ctx, err, "fanout timeline write failed", "post_id", event.PostID, "follower_id", followerID)
			continue
		}
		succeeded++
	}
	duration := time.Since(start)
	ObserveFanoutProcessed(duration)
	// Surface error when nothing was successfully delivered AND we hit a real
	// failure along the way — covers both "all writes failed" and "ctx cancelled
	// before any write completed". A pure no-op (e.g. zero non-nil followers)
	// stays a success.
	if succeeded == 0 && lastErr != nil {
		return fmt.Errorf("fanout post %s: %d attempted, 0 succeeded: %w", event.PostID, attempted, lastErr)
	}
	logger.Info(ctx, "fanout post processed", "post_id", event.PostID, "author_id", event.AuthorID, "followers", len(followers), "succeeded", succeeded, "failed", failed, "duration_ms", duration.Milliseconds())
	return nil
}
