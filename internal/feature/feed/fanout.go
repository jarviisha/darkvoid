package feed

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// FanoutWorker writes post-created events into followers' prepared timelines.
//
// The follower cap is read from settings per event rather than captured here, so
// lowering it takes effect on the next post instead of the next restart — which
// is when an operator reaches for it: a single very-followed account flooding the
// queue is a live incident, not a deployment.
type FanoutWorker struct {
	followerReader FollowerReader
	timeline       TimelineStore
	settings       *Settings
}

func NewFanoutWorker(followerReader FollowerReader, timeline TimelineStore, settings *Settings) *FanoutWorker {
	return &FanoutWorker{
		followerReader: followerReader,
		timeline:       timeline,
		settings:       settings,
	}
}

// maxFollowers is the current cap. A non-positive stored value falls back to the
// default rather than to "no followers": the column's CHECK keeps it at 1 or more,
// so reaching zero here would mean the settings were never loaded, and silently
// fanning out to nobody is the one outcome that looks like success.
func (w *FanoutWorker) maxFollowers() int {
	if n := w.settings.Get().FanoutMaxFollowers; n > 0 {
		return n
	}
	return DefaultRuntimeSettings().FanoutMaxFollowers
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
	followerCap := w.maxFollowers()
	if len(followers) > followerCap {
		followers = followers[:followerCap]
		CountFanoutCapped()
		logger.Info(ctx, "fanout follower list capped", "post_id", event.PostID, "author_id", event.AuthorID, "followers", originalFollowerCount, "cap", followerCap)
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
