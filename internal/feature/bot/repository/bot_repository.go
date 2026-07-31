package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/internal/feature/bot/db"
	"github.com/jarviisha/darkvoid/internal/feature/bot/entity"
	"github.com/jarviisha/darkvoid/internal/infrastructure/database"
	apperrors "github.com/jarviisha/darkvoid/pkg/errors"
)

// BotRepository is the only thing that knows the bot schema. It maps the sqlc row
// types to entities so the service works in time.Duration and *uuid.UUID rather
// than pgtype values.
type BotRepository struct {
	queries *db.Queries
}

func NewBotRepository(pool *pgxpool.Pool) *BotRepository {
	return &BotRepository{queries: db.New(pool)}
}

// ─── Personas ────────────────────────────────────────────────────────────────

func (r *BotRepository) ListBots(ctx context.Context) ([]*entity.Bot, error) {
	rows, err := r.queries.ListBots(ctx)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return mapRows(rows, rowToBot), nil
}

func (r *BotRepository) GetBot(ctx context.Context, id uuid.UUID) (*entity.Bot, error) {
	row, err := r.queries.GetBot(ctx, id)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return rowToBot(row), nil
}

func (r *BotRepository) GetBotByUsername(ctx context.Context, username string) (*entity.Bot, error) {
	row, err := r.queries.GetBotByUsername(ctx, username)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return rowToBot(row), nil
}

// ListEnabledBots returns at most limit enabled personas, those with a pending run
// request first and the rest by username. The ordering is what guarantees an
// operator's run-now reaches the plan even for a persona that sorts past the cap.
func (r *BotRepository) ListEnabledBots(ctx context.Context, limit int32) ([]*entity.Bot, error) {
	rows, err := r.queries.ListEnabledBots(ctx, limit)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return mapRows(rows, rowToBot), nil
}

func (r *BotRepository) CreateBot(ctx context.Context, username, displayName, style string) (*entity.Bot, error) {
	row, err := r.queries.CreateBot(ctx, db.CreateBotParams{
		Username:    username,
		DisplayName: displayName,
		Style:       style,
	})
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return rowToBot(row), nil
}

// UpdateBot applies a partial update: a nil argument keeps the stored value.
func (r *BotRepository) UpdateBot(ctx context.Context, id uuid.UUID, displayName, style *string, enabled *bool) (*entity.Bot, error) {
	row, err := r.queries.UpdateBot(ctx, db.UpdateBotParams{
		ID:          id,
		DisplayName: displayName,
		Style:       style,
		Enabled:     enabled,
	})
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return rowToBot(row), nil
}

func (r *BotRepository) DeleteBot(ctx context.Context, id uuid.UUID) error {
	return database.MapDBError(r.queries.DeleteBot(ctx, id))
}

func (r *BotRepository) LinkBotUser(ctx context.Context, id, userID uuid.UUID) error {
	return database.MapDBError(r.queries.LinkBotUser(ctx, db.LinkBotUserParams{
		ID:     id,
		UserID: pgtype.UUID{Bytes: userID, Valid: true},
	}))
}

func (r *BotRepository) RequestBotRun(ctx context.Context, id uuid.UUID) (*entity.Bot, error) {
	row, err := r.queries.RequestBotRun(ctx, id)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return rowToBot(row), nil
}

func (r *BotRepository) ClearBotRunRequest(ctx context.Context, id uuid.UUID) error {
	return database.MapDBError(r.queries.ClearBotRunRequest(ctx, id))
}

// ─── Config ──────────────────────────────────────────────────────────────────

func (r *BotRepository) GetConfig(ctx context.Context) (*entity.Config, error) {
	row, err := r.queries.GetBotConfig(ctx)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return rowToConfig(row), nil
}

func (r *BotRepository) UpdateConfig(ctx context.Context, update entity.ConfigUpdate) (*entity.Config, error) {
	params := db.UpdateBotConfigParams{
		Models: update.Models,
		Paused: update.Paused,
	}
	if update.PostInterval != nil {
		seconds := entity.DurationToSeconds(*update.PostInterval)
		params.PostIntervalSeconds = &seconds
	}
	if update.Accounts != nil {
		accounts := int16(*update.Accounts) //nolint:gosec // bounded by the service's MaxAccounts check
		params.Accounts = &accounts
	}
	// Passed through as-is, empty string included: '' is how an operator reverts to
	// the bot's built-in prompt, so only a nil pointer may mean "unchanged".
	params.PromptTemplate = update.PromptTemplate
	params.Temperature = update.Temperature
	if update.MaxTagsPerPost != nil {
		maxTags := int16(*update.MaxTagsPerPost) //nolint:gosec // bounded by the service's MaxTagsCeiling check
		params.MaxTagsPerPost = &maxTags
	}
	if update.RecentMemory != nil {
		recent := int16(*update.RecentMemory) //nolint:gosec // bounded by the service's MaxRecentMemory check
		params.RecentMemory = &recent
	}
	if update.APITimeout != nil {
		seconds := int16(entity.DurationToSeconds(*update.APITimeout)) //nolint:gosec // bounded by the service's MaxTimeout check
		params.ApiTimeoutSeconds = &seconds
	}
	if update.GeminiTimeout != nil {
		seconds := int16(entity.DurationToSeconds(*update.GeminiTimeout)) //nolint:gosec // bounded by the service's MaxTimeout check
		params.GeminiTimeoutSeconds = &seconds
	}
	if update.UpdatedBy != nil {
		params.UpdatedBy = pgtype.UUID{Bytes: *update.UpdatedBy, Valid: true}
	}

	row, err := r.queries.UpdateBotConfig(ctx, params)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return rowToConfig(row), nil
}

// ─── Topics ──────────────────────────────────────────────────────────────────

func (r *BotRepository) ListTopics(ctx context.Context) ([]*entity.Topic, error) {
	rows, err := r.queries.ListTopics(ctx)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return mapRows(rows, rowToTopic), nil
}

func (r *BotRepository) ListEnabledTopics(ctx context.Context) ([]*entity.Topic, error) {
	rows, err := r.queries.ListEnabledTopics(ctx)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return mapRows(rows, rowToTopic), nil
}

func (r *BotRepository) CreateTopic(ctx context.Context, content string) (*entity.Topic, error) {
	row, err := r.queries.CreateTopic(ctx, content)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return rowToTopic(row), nil
}

func (r *BotRepository) SetTopicEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*entity.Topic, error) {
	row, err := r.queries.SetTopicEnabled(ctx, db.SetTopicEnabledParams{ID: id, Enabled: enabled})
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return rowToTopic(row), nil
}

func (r *BotRepository) DeleteTopic(ctx context.Context, id uuid.UUID) error {
	deleted, err := r.queries.DeleteTopic(ctx, id)
	if err != nil {
		return database.MapDBError(err)
	}
	// A zero-row delete means the id is unknown; surface it like the other topic
	// writes do instead of reporting success for a deletion that did nothing.
	if deleted == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

// ─── Activity log ────────────────────────────────────────────────────────────

func (r *BotRepository) CreateRun(ctx context.Context, run entity.NewRun) (*entity.Run, error) {
	row, err := r.queries.CreateRun(ctx, db.CreateRunParams{
		BotID:     run.BotID,
		PostID:    uuidToNullable(run.PostID),
		ModelUsed: run.ModelUsed,
		Status:    run.Status.String(),
		Error:     run.Error,
	})
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return rowToRun(row), nil
}

// ListRuns returns the activity log newest first. A nil botID means every persona.
func (r *BotRepository) ListRuns(ctx context.Context, botID *uuid.UUID, limit, offset int32) ([]*entity.Run, error) {
	rows, err := r.queries.ListRuns(ctx, db.ListRunsParams{
		BotID:     uuidToNullable(botID),
		RowLimit:  limit,
		RowOffset: offset,
	})
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return mapRows(rows, rowToRun), nil
}

func (r *BotRepository) CountRuns(ctx context.Context, botID *uuid.UUID) (int64, error) {
	count, err := r.queries.CountRuns(ctx, uuidToNullable(botID))
	if err != nil {
		return 0, database.MapDBError(err)
	}
	return count, nil
}

// ListRunStats returns one summary row per persona with runs inside the retention
// window. Personas that have never run, or whose runs have all aged out, are absent
// rather than zeroed.
func (r *BotRepository) ListRunStats(ctx context.Context, since time.Time) ([]*entity.RunStats, error) {
	rows, err := r.queries.ListBotRunStats(ctx, pgtype.Timestamptz{Time: since, Valid: true})
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return mapRows(rows, rowToRunStats), nil
}

// PruneRuns drops runs older than the cutoff and reports how many went. Errors are
// the caller's to decide on: pruning is maintenance, not part of recording a run.
func (r *BotRepository) PruneRuns(ctx context.Context, before time.Time) (int64, error) {
	deleted, err := r.queries.PruneRuns(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return 0, database.MapDBError(err)
	}
	return deleted, nil
}

// ─── Row mapping ─────────────────────────────────────────────────────────────

// mapRows converts a slice of sqlc rows with a per-row mapper, so each list method
// stays a single call rather than repeating the loop.
func mapRows[R any, E any](rows []R, convert func(R) *E) []*E {
	out := make([]*E, len(rows))
	for i, row := range rows {
		out[i] = convert(row)
	}
	return out
}

func rowToBot(row db.BotBot) *entity.Bot {
	return &entity.Bot{
		ID:             row.ID,
		Username:       row.Username,
		DisplayName:    row.DisplayName,
		Style:          row.Style,
		Enabled:        row.Enabled,
		UserID:         nullableToUUID(row.UserID),
		RunRequestedAt: nullableToTime(row.RunRequestedAt),
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}
}

func rowToConfig(row db.BotConfig) *entity.Config {
	return &entity.Config{
		PostInterval:   time.Duration(row.PostIntervalSeconds) * time.Second,
		Accounts:       int(row.Accounts),
		Models:         row.Models,
		Paused:         row.Paused,
		PromptTemplate: row.PromptTemplate,
		Temperature:    row.Temperature,
		MaxTagsPerPost: int(row.MaxTagsPerPost),
		RecentMemory:   int(row.RecentMemory),
		APITimeout:     time.Duration(row.ApiTimeoutSeconds) * time.Second,
		GeminiTimeout:  time.Duration(row.GeminiTimeoutSeconds) * time.Second,
		UpdatedBy:      nullableToUUID(row.UpdatedBy),
		UpdatedAt:      row.UpdatedAt.Time,
	}
}

func rowToTopic(row db.BotTopic) *entity.Topic {
	return &entity.Topic{
		ID:        row.ID,
		Content:   row.Content,
		Enabled:   row.Enabled,
		CreatedAt: row.CreatedAt.Time,
	}
}

func rowToRun(row db.BotRun) *entity.Run {
	return &entity.Run{
		ID:        row.ID,
		BotID:     row.BotID,
		PostID:    nullableToUUID(row.PostID),
		ModelUsed: row.ModelUsed,
		Status:    entity.RunStatus(row.Status),
		Error:     row.Error,
		CreatedAt: row.CreatedAt.Time,
	}
}

func rowToRunStats(row db.ListBotRunStatsRow) *entity.RunStats {
	return &entity.RunStats{
		BotID:            row.BotID,
		LastSuccessAt:    nullableToTime(row.LastSuccessAt),
		LastErrorAt:      nullableToTime(row.LastErrorAt),
		SuccessesLast24h: row.SuccessesLast24h,
		ErrorsLast24h:    row.ErrorsLast24h,
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

func nullableToTime(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}
