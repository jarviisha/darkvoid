package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/bot/entity"
	"github.com/jarviisha/darkvoid/internal/pagination"
)

// botStore is the narrow interface the bot service needs from its repository.
type botStore interface {
	ListBots(ctx context.Context) ([]*entity.Bot, error)
	GetBot(ctx context.Context, id uuid.UUID) (*entity.Bot, error)
	GetBotByUsername(ctx context.Context, username string) (*entity.Bot, error)
	// ListEnabledBots returns at most limit enabled personas, those with a pending
	// run request first — the plan carries only `accounts` personas, so a request for
	// one sorting past the cap would otherwise never be seen.
	ListEnabledBots(ctx context.Context, limit int32) ([]*entity.Bot, error)
	CreateBot(ctx context.Context, username, displayName, style string) (*entity.Bot, error)
	UpdateBot(ctx context.Context, id uuid.UUID, displayName, style *string, enabled *bool) (*entity.Bot, error)
	DeleteBot(ctx context.Context, id uuid.UUID) error
	LinkBotUser(ctx context.Context, id, userID uuid.UUID) error
	RequestBotRun(ctx context.Context, id uuid.UUID) (*entity.Bot, error)
	ClearBotRunRequest(ctx context.Context, id uuid.UUID) error

	GetConfig(ctx context.Context) (*entity.Config, error)
	UpdateConfig(ctx context.Context, update entity.ConfigUpdate) (*entity.Config, error)

	ListTopics(ctx context.Context) ([]*entity.Topic, error)
	ListEnabledTopics(ctx context.Context) ([]*entity.Topic, error)
	CreateTopic(ctx context.Context, content string) (*entity.Topic, error)
	SetTopicEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*entity.Topic, error)
	DeleteTopic(ctx context.Context, id uuid.UUID) error

	CreateRun(ctx context.Context, run entity.NewRun) (*entity.Run, error)
	ListRuns(ctx context.Context, botID *uuid.UUID, limit, offset int32) ([]*entity.Run, error)
	CountRuns(ctx context.Context, botID *uuid.UUID) (int64, error)
	ListRunStats(ctx context.Context) ([]*entity.RunStats, error)
}

// postReader is the narrow interface the activity log needs from the post feature,
// implemented by an adapter in internal/app. It is batched on purpose: resolving
// one post per log row would be a query per row.
//
// Ids with no surviving post are simply absent from the result — a bot's post can
// be deleted, and the log entry that recorded it must stay readable.
type postReader interface {
	GetPostPreviews(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

// RunFilter holds the filter and pagination options for reading the activity log.
type RunFilter struct {
	pagination.PaginationRequest
	// BotID limits the log to one persona; nil means every persona.
	BotID *uuid.UUID
}
