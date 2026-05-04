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
type EventDispatcher struct {
	enabled bool
	jobs    chan Event
	handler EventHandler
	closed  atomic.Bool
	wg      sync.WaitGroup
}

func NewEventDispatcher(enabled bool, workers int, queueSize int, handler EventHandler) *EventDispatcher {
	if workers <= 0 {
		workers = 1
	}
	if queueSize <= 0 {
		queueSize = 1
	}
	d := &EventDispatcher{
		enabled: enabled,
		jobs:    make(chan Event, queueSize),
		handler: handler,
	}
	if enabled && handler != nil {
		for i := 0; i < workers; i++ {
			d.wg.Add(1)
			go d.worker()
		}
	}
	return d
}

func (d *EventDispatcher) Dispatch(ctx context.Context, event Event) bool {
	if d == nil || !d.enabled || d.handler == nil || d.closed.Load() {
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
		ctx := context.Background()
		if err := d.handler.HandleFeedEvent(ctx, event); err != nil {
			logger.LogError(ctx, err, "feed event handling failed", "event_type", event.Type)
		}
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
		Score:      TimelineScoreFromTime(createdAt),
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
