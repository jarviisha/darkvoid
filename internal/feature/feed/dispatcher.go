package feed

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// EventType identifies feed-impacting mutations.
type EventType string

const (
	EventPostCreated       EventType = "post_created"
	EventFollowCreated     EventType = "follow_created"
	EventFollowDeleted     EventType = "follow_deleted"
	EventPostDeleted       EventType = "post_deleted"
	EventVisibilityChanged EventType = "post_visibility_changed"
)

// eventHandlerTimeout caps per-event handler execution. Sized for fanout to
// the configured maxFollowers cap (~10K serial Redis writes ≈ 10s) with headroom.
const eventHandlerTimeout = 30 * time.Second

// Event is one feed-impacting mutation.
type Event struct {
	Type       EventType
	PostID     uuid.UUID
	AuthorID   uuid.UUID
	ActorID    uuid.UUID
	FolloweeID uuid.UUID
	Visibility string
	CreatedAt  time.Time
	Score      int64
}

// EventHandler handles one feed event.
type EventHandler interface {
	HandleFeedEvent(ctx context.Context, event Event) error
}

// EventDispatcher dispatches feed events to in-process workers.
//
// Whether fanout is enabled is read from settings per dispatch, not captured at
// construction — that is what makes it a kill switch rather than a deploy-time
// choice. The workers are therefore started whenever a handler is present, even
// with fanout currently off: an idle worker blocked on an empty channel costs
// nothing, and starting them lazily would mean the first event after an operator
// flips the switch races the pool coming up.
//
// The two sizing knobs, workers and queueSize, stay construction-time arguments
// fed from the environment. They allocate goroutines and a channel, so no stored
// value could take effect without rebuilding this — and a knob that quietly does
// nothing until the next restart is worse than one that says it needs one.
type EventDispatcher struct {
	settings *Settings
	jobs     chan Event
	handler  EventHandler
	closed   atomic.Bool
	wg       sync.WaitGroup
}

func NewEventDispatcher(settings *Settings, workers int, queueSize int, handler EventHandler) *EventDispatcher {
	if workers <= 0 {
		workers = 1
	}
	if queueSize <= 0 {
		queueSize = 1
	}
	d := &EventDispatcher{
		settings: settings,
		jobs:     make(chan Event, queueSize),
		handler:  handler,
	}
	if handler != nil {
		for i := 0; i < workers; i++ {
			d.wg.Add(1)
			go d.worker()
		}
	}
	return d
}

// writeScore is the rank score given to a brand-new post at fan-out time.
//
// For a fresh post the local formula degenerates to exactly
// RecencyScale + RelationshipBonus (zero likes, zero age, author followed by
// every fan-out recipient), so the constant is used openly; the createdAt
// component of the packed score keeps fan-out writes newest-first inside this
// shared rank bucket. Computed per emit rather than once, so that a weight change
// moves fan-out writes and read-path ranking together — the identity above is
// only true while both use the same numbers.
func (d *EventDispatcher) writeScore() float64 {
	cfg := d.settings.Get().Scorer
	return cfg.RecencyScale + cfg.RelationshipBonus
}

func (d *EventDispatcher) Dispatch(ctx context.Context, event Event) bool {
	if d == nil || d.handler == nil || d.closed.Load() || !d.settings.Get().FanoutEnabled {
		return false
	}
	select {
	case d.jobs <- event:
		SetDispatchQueueDepth(len(d.jobs))
		return true
	default:
		CountDispatchEnqueueFailed()
		SetDispatchQueueDepth(len(d.jobs))
		logger.Warn(ctx, "feed event queue full", "event_type", event.Type, "queue_depth", len(d.jobs))
		return false
	}
}

func (d *EventDispatcher) Close() {
	if d == nil || d.closed.Swap(true) {
		return
	}
	close(d.jobs)
	d.wg.Wait()
}

func (d *EventDispatcher) worker() {
	defer d.wg.Done()
	for event := range d.jobs {
		SetDispatchQueueDepth(len(d.jobs))
		d.handleEvent(event)
	}
}

func (d *EventDispatcher) handleEvent(event Event) {
	ctx, cancel := context.WithTimeout(context.Background(), eventHandlerTimeout)
	defer cancel()
	if err := d.handler.HandleFeedEvent(ctx, event); err != nil {
		logger.LogError(ctx, err, "feed event handling failed", "event_type", event.Type)
	}
}

// EmitPostCreated publishes a post-created feed event.
func (d *EventDispatcher) EmitPostCreated(ctx context.Context, postID, authorID uuid.UUID, visibility string, createdAt time.Time) error {
	d.Dispatch(ctx, Event{
		Type:       EventPostCreated,
		PostID:     postID,
		AuthorID:   authorID,
		Visibility: visibility,
		CreatedAt:  createdAt,
		Score:      PackTimelineScore(d.writeScore(), createdAt),
	})
	return nil
}

// EmitFollowCreated publishes a follow-created feed event.
func (d *EventDispatcher) EmitFollowCreated(ctx context.Context, followerID, followeeID uuid.UUID) error {
	d.Dispatch(ctx, Event{Type: EventFollowCreated, ActorID: followerID, FolloweeID: followeeID, CreatedAt: time.Now().UTC()})
	return nil
}

// EmitFollowDeleted publishes a follow-deleted feed event.
func (d *EventDispatcher) EmitFollowDeleted(ctx context.Context, followerID, followeeID uuid.UUID) error {
	d.Dispatch(ctx, Event{Type: EventFollowDeleted, ActorID: followerID, FolloweeID: followeeID, CreatedAt: time.Now().UTC()})
	return nil
}
