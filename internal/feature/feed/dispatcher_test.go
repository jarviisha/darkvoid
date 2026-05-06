package feed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingEventHandler struct {
	events chan Event
	err    error
}

func (h *recordingEventHandler) HandleFeedEvent(_ context.Context, event Event) error {
	if h.events != nil {
		h.events <- event
	}
	return h.err
}

func TestEventDispatcher_DisabledDoesNotEnqueue(t *testing.T) {
	handler := &recordingEventHandler{events: make(chan Event, 1)}
	dispatcher := NewEventDispatcher(false, 1, 1, handler)

	if dispatcher.Dispatch(context.Background(), Event{Type: EventPostCreated}) {
		t.Fatal("disabled dispatcher should not enqueue")
	}
	if len(handler.events) != 0 {
		t.Fatal("disabled dispatcher handled an event")
	}
}

func TestEventDispatcher_EnqueueSuccess(t *testing.T) {
	handler := &recordingEventHandler{events: make(chan Event, 1)}
	dispatcher := NewEventDispatcher(true, 1, 1, handler)
	defer dispatcher.Close()

	postID := uuid.New()
	if !dispatcher.Dispatch(context.Background(), Event{Type: EventPostCreated, PostID: postID}) {
		t.Fatal("expected dispatch success")
	}
	select {
	case got := <-handler.events:
		if got.PostID != postID {
			t.Fatalf("post ID = %s, want %s", got.PostID, postID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventDispatcher_FullQueueReturnsFalse(t *testing.T) {
	dispatcher := &EventDispatcher{
		enabled: true,
		jobs:    make(chan Event, 1),
		handler: &recordingEventHandler{},
	}
	dispatcher.jobs <- Event{Type: EventPostCreated}

	if dispatcher.Dispatch(context.Background(), Event{Type: EventPostCreated}) {
		t.Fatal("expected full queue dispatch to return false")
	}
}

func TestEventDispatcher_HandlerErrorIsNonFatal(t *testing.T) {
	handler := &recordingEventHandler{events: make(chan Event, 1), err: errors.New("boom")}
	dispatcher := NewEventDispatcher(true, 1, 1, handler)
	defer dispatcher.Close()

	if !dispatcher.Dispatch(context.Background(), Event{Type: EventPostCreated}) {
		t.Fatal("expected dispatch success despite handler error")
	}
	select {
	case <-handler.events:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

// deadlineCapturingHandler records the context's deadline so tests can
// assert the worker installs eventHandlerTimeout per event.
type deadlineCapturingHandler struct {
	done     chan struct{}
	hasDL    bool
	deadline time.Time
}

func (h *deadlineCapturingHandler) HandleFeedEvent(ctx context.Context, _ Event) error {
	h.deadline, h.hasDL = ctx.Deadline()
	close(h.done)
	return nil
}

func TestEventDispatcher_WorkerAppliesPerEventTimeout(t *testing.T) {
	handler := &deadlineCapturingHandler{done: make(chan struct{})}
	dispatcher := NewEventDispatcher(true, 1, 1, handler)
	defer dispatcher.Close()

	start := time.Now()
	if !dispatcher.Dispatch(context.Background(), Event{Type: EventPostCreated}) {
		t.Fatal("dispatch failed")
	}
	select {
	case <-handler.done:
	case <-time.After(time.Second):
		t.Fatal("handler did not run")
	}
	if !handler.hasDL {
		t.Fatal("worker context has no deadline; expected eventHandlerTimeout")
	}
	remaining := time.Until(handler.deadline)
	// Deadline should be eventHandlerTimeout (30s) minus a tiny scheduling delta.
	if remaining <= 0 || remaining > eventHandlerTimeout {
		t.Fatalf("deadline %v out of range (now=%v, timeout=%v)", handler.deadline, start, eventHandlerTimeout)
	}
}

// blockingHandler blocks until released, letting tests inspect graceful shutdown.
type blockingHandler struct {
	started chan struct{}
	release chan struct{}
	done    chan struct{}
}

func (h *blockingHandler) HandleFeedEvent(_ context.Context, _ Event) error {
	close(h.started)
	<-h.release
	close(h.done)
	return nil
}

func TestEventDispatcher_CloseDrainsInFlightEvents(t *testing.T) {
	handler := &blockingHandler{
		started: make(chan struct{}),
		release: make(chan struct{}),
		done:    make(chan struct{}),
	}
	dispatcher := NewEventDispatcher(true, 1, 1, handler)

	if !dispatcher.Dispatch(context.Background(), Event{Type: EventPostCreated}) {
		t.Fatal("dispatch failed")
	}
	<-handler.started // worker is now blocked inside the handler

	closeReturned := make(chan struct{})
	go func() {
		dispatcher.Close()
		close(closeReturned)
	}()

	// Close must NOT return while the handler is still running.
	select {
	case <-closeReturned:
		t.Fatal("Close returned before in-flight handler finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(handler.release)
	select {
	case <-closeReturned:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after handler completed")
	}
	select {
	case <-handler.done:
	default:
		t.Fatal("handler did not finish before Close returned")
	}
}

func TestEventDispatcher_DispatchAfterCloseReturnsFalse(t *testing.T) {
	dispatcher := NewEventDispatcher(true, 1, 1, &recordingEventHandler{})
	dispatcher.Close()
	if dispatcher.Dispatch(context.Background(), Event{Type: EventPostCreated}) {
		t.Fatal("expected dispatch after Close to return false")
	}
}
