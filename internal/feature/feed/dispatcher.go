package feed

import (
	"context"
	"errors"
	"sync"
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

var errFeedEventNotDispatched = errors.New("feed event was not dispatched")

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
	// mu guards closed and, on the read side, spans the closed check and the
	// channel send in Dispatch. Close closes the channel under the write lock,
	// so no sender can sit between its check and its send when the channel
	// closes — with a bare atomic flag that window is a send-on-closed-channel
	// panic on any request racing a shutdown.
	mu             sync.RWMutex
	closed         bool
	wg             sync.WaitGroup
	outbox         *PostgresOutbox
	stopOutbox     chan struct{}
	outboxWG       sync.WaitGroup
	outboxStopOnce sync.Once
}

// WithOutbox starts the durable consumer. Call once during application wiring.
func (d *EventDispatcher) WithOutbox(outbox *PostgresOutbox) {
	if d == nil || outbox == nil || d.outbox != nil {
		return
	}
	d.outbox = outbox
	d.stopOutbox = make(chan struct{})
	d.outboxWG.Add(1)
	go d.consumeOutbox()
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
	if d == nil || d.handler == nil || !d.settings.Get().FanoutEnabled {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.closed {
		return false
	}
	select {
	case d.jobs <- event:
		SetDispatchQueueDepth(len(d.jobs))
		return true
	default:
		CountDispatchEnqueueFailed()
		SetDispatchQueueDepth(len(d.jobs))
		logger.Warn(ctx, "feed event queue full; handling synchronously", "event_type", event.Type, "queue_depth", len(d.jobs))
		if err := d.handler.HandleFeedEvent(ctx, event); err != nil {
			logger.LogError(ctx, err, "synchronous feed event fallback failed", "event_type", event.Type)
			return false
		}
		return true
	}
}

// Close stops intake and blocks until every already-queued event has been
// handled. Call it before closing the stores the handlers write to, or the
// drain fails every remaining write.
func (d *EventDispatcher) Close() {
	if d == nil {
		return
	}
	d.outboxStopOnce.Do(func() {
		if d.stopOutbox != nil {
			close(d.stopOutbox)
			d.outboxWG.Wait()
		}
	})
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	close(d.jobs)
	d.mu.Unlock()
	d.wg.Wait()
}

func (d *EventDispatcher) consumeOutbox() {
	defer d.outboxWG.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-d.stopOutbox:
			return
		case <-ticker.C:
			if !d.settings.Get().FanoutEnabled {
				continue
			}
			d.consumeOutboxBatch()
		}
	}
}

func (d *EventDispatcher) consumeOutboxBatch() {
	ctx, cancel := context.WithTimeout(context.Background(), eventHandlerTimeout)
	defer cancel()
	entries, err := d.outbox.claim(ctx, 50)
	if err != nil {
		logger.LogError(ctx, err, "feed outbox claim failed")
		return
	}
	for _, entry := range entries {
		if entry.Event.Score == 0 && (entry.Event.Type == EventPostCreated || entry.Event.Type == EventVisibilityChanged) {
			entry.Event.Score = PackTimelineScore(d.writeScore(), entry.Event.CreatedAt)
		}
		if err := d.handler.HandleFeedEvent(ctx, entry.Event); err != nil {
			logger.LogError(ctx, err, "feed outbox event failed", "event_type", entry.Event.Type, "attempt", entry.Attempts)
			CountOutboxRetry()
			if entry.Attempts >= outboxMaxAttempts {
				CountOutboxDeadLetter()
			}
			if failErr := d.outbox.fail(ctx, entry, err); failErr != nil {
				logger.LogError(ctx, failErr, "feed outbox retry scheduling failed", "event_id", entry.ID)
			}
			continue
		}
		if err := d.outbox.complete(ctx, entry.ID); err != nil {
			logger.LogError(ctx, err, "feed outbox completion failed", "event_id", entry.ID)
		}
	}
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
	if !d.Dispatch(ctx, Event{
		Type:       EventPostCreated,
		PostID:     postID,
		AuthorID:   authorID,
		Visibility: visibility,
		CreatedAt:  createdAt,
		Score:      PackTimelineScore(d.writeScore(), createdAt),
	}) {
		return errFeedEventNotDispatched
	}
	return nil
}

func (d *EventDispatcher) EmitPostDeleted(ctx context.Context, postID, authorID uuid.UUID) error {
	if !d.Dispatch(ctx, Event{Type: EventPostDeleted, PostID: postID, AuthorID: authorID}) {
		return errFeedEventNotDispatched
	}
	return nil
}

func (d *EventDispatcher) EmitPostVisibilityChanged(ctx context.Context, postID, authorID uuid.UUID, visibility string, createdAt time.Time) error {
	if !d.Dispatch(ctx, Event{
		Type: EventVisibilityChanged, PostID: postID, AuthorID: authorID,
		Visibility: visibility, CreatedAt: createdAt,
		Score: PackTimelineScore(d.writeScore(), createdAt),
	}) {
		return errFeedEventNotDispatched
	}
	return nil
}

// EmitFollowCreated publishes a follow-created feed event.
func (d *EventDispatcher) EmitFollowCreated(ctx context.Context, followerID, followeeID uuid.UUID) error {
	if !d.Dispatch(ctx, Event{Type: EventFollowCreated, ActorID: followerID, FolloweeID: followeeID, CreatedAt: time.Now().UTC()}) {
		return errFeedEventNotDispatched
	}
	return nil
}

// EmitFollowDeleted publishes a follow-deleted feed event.
func (d *EventDispatcher) EmitFollowDeleted(ctx context.Context, followerID, followeeID uuid.UUID) error {
	if !d.Dispatch(ctx, Event{Type: EventFollowDeleted, ActorID: followerID, FolloweeID: followeeID, CreatedAt: time.Now().UTC()}) {
		return errFeedEventNotDispatched
	}
	return nil
}
