package service

import (
	"context"

	"github.com/jarviisha/darkvoid/internal/feature/settings/entity"
)

// settingsRepository is the persistence this service needs, named on the consumer
// side so the service can be tested without a database.
type settingsRepository interface {
	GetFeedSettings(ctx context.Context) (*entity.FeedSettings, error)
	UpdateFeedSettings(ctx context.Context, update entity.FeedSettingsUpdate) (*entity.FeedSettings, error)
}

// FeedSettingsSink receives every settings snapshot this service reads or writes.
//
// It is defined here, on the producer's side, because it is a callback rather
// than a dependency: this context has no idea what a feed is, and the
// implementation lives in internal/app where the two contexts are allowed to
// meet. Same shape as FollowService.WithFeedInvalidator.
//
// Implementations are called from request goroutines (on update) and from the
// refresh loop, so they must be safe for concurrent use and must not block —
// applying a snapshot is a pointer swap, not a fan-out.
type FeedSettingsSink interface {
	ApplyFeedSettings(settings entity.FeedSettings)
}
