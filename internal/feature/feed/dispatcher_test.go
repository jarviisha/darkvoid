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
