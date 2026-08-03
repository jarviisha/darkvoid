package handler

import (
	"context"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/settings/dto"
)

// settingsService is the narrow view of the service this handler needs, named on
// the consumer side so the handler can be tested against a stub.
type settingsService interface {
	GetFeedSettings(ctx context.Context) (*dto.FeedSettingsResponse, error)
	UpdateFeedSettings(ctx context.Context, req *dto.UpdateFeedSettingsRequest, adminID uuid.UUID) (*dto.FeedSettingsResponse, error)
}
