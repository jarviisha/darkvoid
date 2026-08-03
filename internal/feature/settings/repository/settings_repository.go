package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/internal/feature/settings/db"
	"github.com/jarviisha/darkvoid/internal/feature/settings/entity"
	"github.com/jarviisha/darkvoid/internal/infrastructure/database"
)

// SettingsRepository is the only thing that knows the settings schema. It maps
// the sqlc row types to entities so the service works in time.Duration, int and
// *uuid.UUID rather than in the column widths.
type SettingsRepository struct {
	queries *db.Queries
}

func NewSettingsRepository(pool *pgxpool.Pool) *SettingsRepository {
	return &SettingsRepository{queries: db.New(pool)}
}

// GetFeedSettings reads the single settings.feed row. The row is created by the
// migration, so a no-rows error here means the migration has not run — which is
// a real failure to report, not a case to paper over with defaults.
func (r *SettingsRepository) GetFeedSettings(ctx context.Context) (*entity.FeedSettings, error) {
	row, err := r.queries.GetFeedSettings(ctx)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return rowToFeedSettings(row), nil
}

// UpdateFeedSettings applies a partial update and returns the row as stored. The
// caller gets the full new state back rather than its own request echoed, so what
// it hands to the feed is what the database actually holds.
func (r *SettingsRepository) UpdateFeedSettings(ctx context.Context, update entity.FeedSettingsUpdate) (*entity.FeedSettings, error) {
	params := db.UpdateFeedSettingsParams{
		TimelineEnabled:       update.TimelineEnabled,
		TimelineRefreshOnMiss: update.TimelineRefreshOnMiss,
		FanoutEnabled:         update.FanoutEnabled,
		RelationshipBonus:     update.RelationshipBonus,
		RecencyScale:          update.RecencyScale,
		DecayExponent:         update.DecayExponent,
		UpdatedBy:             uuidToNullable(update.UpdatedBy),
	}
	// The narrowing conversions below are all guarded by entity.Validate, which the
	// service runs before calling: rollout is 0..100, max items 1..10000, and the
	// TTL is capped at 90 days. A value that reached here unchecked would still hit
	// the column's CHECK — the conversion cannot silently wrap into a legal one.
	if update.TimelineRolloutPercent != nil {
		percent := int16(*update.TimelineRolloutPercent) //nolint:gosec // bounded by entity.FeedSettingsUpdate.Validate
		params.TimelineRolloutPercent = &percent
	}
	if update.TimelineMaxItems != nil {
		items := int32(*update.TimelineMaxItems) //nolint:gosec // bounded by entity.FeedSettingsUpdate.Validate
		params.TimelineMaxItems = &items
	}
	if update.TimelineTTL != nil {
		seconds := entity.DurationToSeconds(*update.TimelineTTL)
		params.TimelineTtlSeconds = &seconds
	}
	if update.FanoutMaxFollowers != nil {
		followers := int32(*update.FanoutMaxFollowers) //nolint:gosec // bounded by entity.FeedSettingsUpdate.Validate
		params.FanoutMaxFollowers = &followers
	}

	row, err := r.queries.UpdateFeedSettings(ctx, params)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return rowToFeedSettings(row), nil
}

func rowToFeedSettings(row db.SettingsFeed) *entity.FeedSettings {
	return &entity.FeedSettings{
		TimelineEnabled:        row.TimelineEnabled,
		TimelineRolloutPercent: int(row.TimelineRolloutPercent),
		TimelineMaxItems:       int(row.TimelineMaxItems),
		TimelineTTL:            entity.SecondsToDuration(row.TimelineTtlSeconds),
		TimelineRefreshOnMiss:  row.TimelineRefreshOnMiss,
		FanoutEnabled:          row.FanoutEnabled,
		FanoutMaxFollowers:     int(row.FanoutMaxFollowers),
		RelationshipBonus:      row.RelationshipBonus,
		RecencyScale:           row.RecencyScale,
		DecayExponent:          row.DecayExponent,
		UpdatedBy:              nullableToUUID(row.UpdatedBy),
		UpdatedAt:              row.UpdatedAt.Time,
	}
}

func uuidToNullable(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}

func nullableToUUID(v pgtype.UUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	id := uuid.UUID(v.Bytes)
	return &id
}
