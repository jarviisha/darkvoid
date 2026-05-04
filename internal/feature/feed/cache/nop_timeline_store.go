package cache

import (
	"context"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
)

// NopTimelineStore is a no-op timeline store used when Redis is disabled.
type NopTimelineStore struct{}

func NewNopTimelineStore() *NopTimelineStore { return &NopTimelineStore{} }

func (n *NopTimelineStore) AddPost(_ context.Context, _ uuid.UUID, _ feed.TimelineEntry) error {
	return nil
}

func (n *NopTimelineStore) AddPostsBatch(_ context.Context, _ uuid.UUID, _ []feed.TimelineEntry) error {
	return nil
}

func (n *NopTimelineStore) ReadPage(_ context.Context, _ uuid.UUID, _ *feed.TimelinePosition, _ int) (*feed.TimelinePage, error) {
	return &feed.TimelinePage{}, nil
}

func (n *NopTimelineStore) Trim(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (n *NopTimelineStore) RemovePostBestEffort(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}
