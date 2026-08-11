package broker

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestBroker_DeliverCleanupShutdownConcurrent(t *testing.T) {
	b := NewBroker(nil)
	userID := uuid.New()
	cleanups := make([]func(), 0, 100)
	for range 100 {
		_, cleanup := b.Subscribe(context.Background(), userID)
		cleanups = append(cleanups, cleanup)
	}

	var wg sync.WaitGroup
	for i := range cleanups {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 100 {
				b.deliverLocal(userID, Event{Type: "notification"})
			}
		}()
		go func(cleanup func()) {
			defer wg.Done()
			cleanup()
			cleanup()
		}(cleanups[i])
	}
	b.Shutdown()
	b.Shutdown()
	wg.Wait()

	ch, cleanup := b.Subscribe(context.Background(), userID)
	cleanup()
	if _, ok := <-ch; ok {
		t.Fatal("subscription after shutdown must be closed")
	}
}
