package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/settings/dto"
	"github.com/jarviisha/darkvoid/internal/feature/settings/entity"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// timeFormat matches the rest of the API's response timestamps.
const timeFormat = "2006-01-02T15:04:05Z"

// SettingsService owns the runtime settings: it reads them, validates and applies
// operator edits, and pushes every resulting snapshot to the registered sinks.
//
// Publishing on read as well as on write is what makes this work with more than
// one API instance. The instance that served the PATCH applies the new snapshot
// immediately; the others only learn about it when their refresh loop next calls
// Refresh. Without the publish-on-read half, a two-instance deployment would
// serve two different rollout percentages until the next restart.
type SettingsService struct {
	repo settingsRepository
	sink FeedSettingsSink
}

func NewSettingsService(repo settingsRepository) *SettingsService {
	return &SettingsService{repo: repo}
}

// WithFeedSettingsSink attaches the destination for feed snapshots. Called after
// setup, because this context is constructed before the feed context exists —
// the same deferred wiring as FollowService.WithFeedInvalidator.
func (s *SettingsService) WithFeedSettingsSink(sink FeedSettingsSink) {
	s.sink = sink
}

// GetFeedSettings returns the stored settings for the admin API.
//
// It reads through to the database rather than returning the last published
// snapshot. The endpoint's job is to show an operator what they are about to
// edit, and a local snapshot can be up to one refresh interval behind an edit
// made on another instance — an operator PATCHing on top of a stale read would
// have their partial update applied to a state they never saw.
func (s *SettingsService) GetFeedSettings(ctx context.Context) (*dto.FeedSettingsResponse, error) {
	settings, err := s.repo.GetFeedSettings(ctx)
	if err != nil {
		return nil, err
	}
	return toFeedSettingsResponse(settings), nil
}

// UpdateFeedSettings validates a partial update, writes it, and publishes the
// stored result.
//
// What is published is the row as the database returned it, not the request: the
// update names some fields and inherits the rest, so only the round trip knows
// the whole new state.
func (s *SettingsService) UpdateFeedSettings(ctx context.Context, req *dto.UpdateFeedSettingsRequest, adminID uuid.UUID) (*dto.FeedSettingsResponse, error) {
	update := toFeedSettingsUpdate(req)
	update.UpdatedBy = &adminID

	if err := update.Validate(); err != nil {
		return nil, err
	}

	settings, err := s.repo.UpdateFeedSettings(ctx, update)
	if err != nil {
		return nil, err
	}

	s.publish(ctx, *settings)
	logger.Info(ctx, "feed settings updated",
		"updated_by", adminID,
		"timeline_enabled", settings.TimelineEnabled,
		"timeline_rollout_percent", settings.TimelineRolloutPercent,
		"fanout_enabled", settings.FanoutEnabled,
	)
	return toFeedSettingsResponse(settings), nil
}

// Refresh re-reads the settings and publishes them. It is what the background
// refresh loop calls, and also what runs once at startup so the feed starts on
// the stored values rather than on the defaults.
//
// An error is returned rather than swallowed so the caller can decide: at startup
// it is logged and the defaults stand; in the loop it is logged and the next tick
// tries again. Neither case is worth failing on — these settings shape the feed,
// they do not gate it.
func (s *SettingsService) Refresh(ctx context.Context) error {
	settings, err := s.repo.GetFeedSettings(ctx)
	if err != nil {
		return err
	}
	s.publish(ctx, *settings)
	return nil
}

func (s *SettingsService) publish(ctx context.Context, settings entity.FeedSettings) {
	if s.sink == nil {
		// Nothing is wired in a unit test, and in production the sink is attached
		// before the first Refresh. Either way there is no snapshot to lose.
		logger.Debug(ctx, "feed settings not published: no sink wired")
		return
	}
	s.sink.ApplyFeedSettings(settings)
}

func toFeedSettingsUpdate(req *dto.UpdateFeedSettingsRequest) entity.FeedSettingsUpdate {
	if req == nil {
		return entity.FeedSettingsUpdate{}
	}
	update := entity.FeedSettingsUpdate{
		TimelineEnabled:       req.TimelineEnabled,
		TimelineRefreshOnMiss: req.TimelineRefreshOnMiss,
		FanoutEnabled:         req.FanoutEnabled,
		RelationshipBonus:     req.RelationshipBonus,
		RecencyScale:          req.RecencyScale,
		DecayExponent:         req.DecayExponent,
	}
	if req.TimelineRolloutPercent != nil {
		percent := int(*req.TimelineRolloutPercent)
		update.TimelineRolloutPercent = &percent
	}
	if req.TimelineMaxItems != nil {
		items := int(*req.TimelineMaxItems)
		update.TimelineMaxItems = &items
	}
	if req.TimelineTTLSeconds != nil {
		ttl := entity.SecondsToDuration(*req.TimelineTTLSeconds)
		update.TimelineTTL = &ttl
	}
	if req.FanoutMaxFollowers != nil {
		followers := int(*req.FanoutMaxFollowers)
		update.FanoutMaxFollowers = &followers
	}
	return update
}

func toFeedSettingsResponse(s *entity.FeedSettings) *dto.FeedSettingsResponse {
	resp := &dto.FeedSettingsResponse{
		TimelineEnabled:        s.TimelineEnabled,
		TimelineRolloutPercent: int32(s.TimelineRolloutPercent), //nolint:gosec // 0..100 by CHECK and by Validate
		TimelineMaxItems:       int32(s.TimelineMaxItems),       //nolint:gosec // 1..10000 by CHECK and by Validate
		TimelineTTLSeconds:     entity.DurationToSeconds(s.TimelineTTL),
		TimelineRefreshOnMiss:  s.TimelineRefreshOnMiss,
		FanoutEnabled:          s.FanoutEnabled,
		FanoutMaxFollowers:     int32(s.FanoutMaxFollowers), //nolint:gosec // stored as INTEGER, read back as int
		RelationshipBonus:      s.RelationshipBonus,
		RecencyScale:           s.RecencyScale,
		DecayExponent:          s.DecayExponent,
		UpdatedAt:              s.UpdatedAt.UTC().Format(timeFormat),
	}
	if s.UpdatedBy != nil {
		id := s.UpdatedBy.String()
		resp.UpdatedBy = &id
	}
	return resp
}
