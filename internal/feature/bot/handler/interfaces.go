package handler

import (
	"context"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/bot/dto"
	"github.com/jarviisha/darkvoid/internal/feature/bot/service"
)

// botService is the narrow interface the handler depends on. Both planes are served
// by the same service: the split is in who is allowed to call which route, not in
// the logic behind them.
type botService interface {
	// Personas
	ListBots(ctx context.Context) (*dto.ListBotsResponse, error)
	CreateBot(ctx context.Context, req *dto.CreateBotRequest) (*dto.BotResponse, error)
	UpdateBot(ctx context.Context, id uuid.UUID, req *dto.UpdateBotRequest) (*dto.BotResponse, error)
	DeleteBot(ctx context.Context, id uuid.UUID) error
	RequestRun(ctx context.Context, id uuid.UUID) (*dto.BotResponse, error)

	// Config
	GetConfig(ctx context.Context) (*dto.ConfigResponse, error)
	UpdateConfig(ctx context.Context, req *dto.UpdateConfigRequest, adminID uuid.UUID) (*dto.ConfigResponse, error)
	SetPaused(ctx context.Context, paused bool, adminID uuid.UUID) (*dto.ConfigResponse, error)

	// Topics
	ListTopics(ctx context.Context) (*dto.ListTopicsResponse, error)
	CreateTopic(ctx context.Context, req *dto.CreateTopicRequest) (*dto.TopicResponse, error)
	SetTopicEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*dto.TopicResponse, error)
	DeleteTopic(ctx context.Context, id uuid.UUID) error

	// Activity log
	ListRuns(ctx context.Context, filter service.RunFilter) (*dto.ListRunsResponse, error)

	// Agent plane
	GetPlan(ctx context.Context) (*dto.PlanResponse, error)
	ReportRun(ctx context.Context, req *dto.ReportRunRequest) error
	LinkIdentity(ctx context.Context, botID uuid.UUID, req *dto.LinkIdentityRequest) error
}
