package feed

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// FanoutWorker maintains prepared timelines from feed events: post-created
// events are written into the author's and followers' timelines, and follow
// changes rebuild the follower's timeline from their current follow graph.
//
// The follower cap is read from settings per event rather than captured here, so
// lowering it takes effect on the next post instead of the next restart — which
// is when an operator reaches for it: a single very-followed account flooding the
// queue is a live incident, not a deployment.
type FanoutWorker struct {
	followerReader FollowerReader
	timeline       TimelineStore
	refresher      TimelineRefresher // optional: nil → follow changes are ignored
	settings       *Settings
}

func NewFanoutWorker(followerReader FollowerReader, timeline TimelineStore, refresher TimelineRefresher, settings *Settings) *FanoutWorker {
	return &FanoutWorker{
		followerReader: followerReader,
		timeline:       timeline,
		refresher:      refresher,
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
	case EventFollowCreated, EventFollowDeleted:
		return w.handleFollowChanged(ctx, event)
	default:
		return nil
	}
}

// handleFollowChanged rebuilds the actor's prepared timeline from their
// current follow graph. A new follow needs the followee's existing posts
// backfilled — fan-out only covers posts created after the follow. Unfollowed
// authors' entries are upserted around rather than deleted: read-side
// eligibility filtering already hides them, and a delete-and-rewrite would
// lose fan-out writes landing between the rebuild's DB read and its store
// write.
func (w *FanoutWorker) handleFollowChanged(ctx context.Context, event Event) error {
	if w.refresher == nil || event.ActorID == uuid.Nil {
		return nil
	}
	if err := w.refresher.RefreshTimeline(ctx, event.ActorID); err != nil {
		CountFanoutError()
		return fmt.Errorf("refresh timeline after follow change: %w", err)
	}
	return nil
}

func (w *FanoutWorker) handlePostCreated(ctx context.Context, event Event) error {
	// A private post never hydrates on the read path (the batch post query
	// filters it out), so writing it anywhere would only plant entries that
	// read as permanently stale.
	if event.Visibility == "private" {
		return nil
	}
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
	// The author reads their own feed from this same prepared timeline, and
	// fan-out is the only writer of fresh posts into it — without the author
	// as a recipient, they are the one user who never sees their own post.
	// Prepended after the cap so a capped follower list cannot squeeze them out.
	recipients := make([]uuid.UUID, 0, len(followers)+1)
	recipients = append(recipients, event.AuthorID)
	recipients = append(recipients, followers...)
	entry := TimelineEntry{PostID: event.PostID, Score: event.Score}
	var attempted, succeeded, failed int
	var lastErr error
	for _, recipientID := range recipients {
		if recipientID == uuid.Nil {
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			lastErr = ctxErr
			break
		}
		attempted++
		if err := w.timeline.AddPost(ctx, recipientID, entry); err != nil {
			CountFanoutError()
			failed++
			lastErr = err
			logger.LogError(ctx, err, "fanout timeline write failed", "post_id", event.PostID, "recipient_id", recipientID)
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
