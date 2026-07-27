package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/bot/dto"
	"github.com/jarviisha/darkvoid/internal/feature/bot/entity"
	"github.com/jarviisha/darkvoid/internal/pagination"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// timeFormat matches the rest of the API's response timestamps.
const timeFormat = "2006-01-02T15:04:05Z"

// previewLength is how much of a post's content the activity log shows. Long
// enough to recognise the post, short enough that the log stays scannable.
const previewLength = 80

// BotService owns the content bot's control plane: the personas, config, and topic
// pool an operator edits, and the activity log the bot reports into.
type BotService struct {
	store botStore
	// posts is optional — nil means the activity log omits post previews rather
	// than failing, so the log works before the post feature is wired.
	posts postReader
}

func NewBotService(store botStore) *BotService {
	return &BotService{store: store}
}

// WithPostReader attaches the post preview source. Called at wire-up time, because
// the bot context cannot depend on the post context at construction.
func (s *BotService) WithPostReader(p postReader) {
	s.posts = p
}

// ─── Personas ────────────────────────────────────────────────────────────────

// ListBots returns every persona with a summary of its recent activity. Personas
// that have never run carry zeroed counts and no timestamps.
func (s *BotService) ListBots(ctx context.Context) (*dto.ListBotsResponse, error) {
	bots, err := s.store.ListBots(ctx)
	if err != nil {
		logger.LogError(ctx, err, "bot: failed to list personas")
		return nil, err
	}

	stats, err := s.store.ListRunStats(ctx)
	if err != nil {
		logger.LogError(ctx, err, "bot: failed to load run stats")
		return nil, err
	}
	byBot := make(map[uuid.UUID]*entity.RunStats, len(stats))
	for _, st := range stats {
		byBot[st.BotID] = st
	}

	data := make([]dto.BotResponse, 0, len(bots))
	for _, b := range bots {
		data = append(data, toBotResponse(b, byBot[b.ID]))
	}
	return &dto.ListBotsResponse{Data: data}, nil
}

// CreateBot adds a persona. The username is checked against the user-service rule
// here so it fails as a validation error rather than a CHECK violation, and
// checked for uniqueness so a duplicate reads as a conflict.
func (s *BotService) CreateBot(ctx context.Context, req *dto.CreateBotRequest) (*dto.BotResponse, error) {
	if !entity.ValidUsername(req.Username) {
		return nil, errors.NewValidationError("username", "must be 3-30 characters of letters, digits, underscore, or hyphen")
	}
	if req.DisplayName == "" {
		return nil, errors.NewValidationError("display_name", "is required")
	}
	if req.Style == "" {
		return nil, errors.NewValidationError("style", "is required — it is the voice instruction sent to the model")
	}

	if _, err := s.store.GetBotByUsername(ctx, req.Username); err == nil {
		return nil, errors.NewConflictError(fmt.Sprintf("bot %q", req.Username))
	} else if !errors.Is(err, errors.ErrNotFound) {
		logger.LogError(ctx, err, "bot: failed to check persona username", "username", req.Username)
		return nil, err
	}

	b, err := s.store.CreateBot(ctx, req.Username, req.DisplayName, req.Style)
	if err != nil {
		logger.LogError(ctx, err, "bot: failed to create persona", "username", req.Username)
		return nil, err
	}

	logger.Info(ctx, "bot: persona created", "bot_id", b.ID, "username", b.Username)
	resp := toBotResponse(b, nil)
	return &resp, nil
}

// UpdateBot applies a partial update to a persona. An update with no fields set is
// rejected rather than silently touching updated_at.
func (s *BotService) UpdateBot(ctx context.Context, id uuid.UUID, req *dto.UpdateBotRequest) (*dto.BotResponse, error) {
	if req.DisplayName == nil && req.Style == nil && req.Enabled == nil {
		return nil, errors.NewBadRequestError("no fields to update")
	}
	if req.DisplayName != nil && *req.DisplayName == "" {
		return nil, errors.NewValidationError("display_name", "cannot be empty")
	}
	if req.Style != nil && *req.Style == "" {
		return nil, errors.NewValidationError("style", "cannot be empty")
	}

	b, err := s.store.UpdateBot(ctx, id, req.DisplayName, req.Style, req.Enabled)
	if err != nil {
		if errors.Is(err, errors.ErrNotFound) {
			return nil, errors.NewNotFoundError("bot")
		}
		logger.LogError(ctx, err, "bot: failed to update persona", "bot_id", id)
		return nil, err
	}

	logger.Info(ctx, "bot: persona updated", "bot_id", id, "username", b.Username)
	resp := toBotResponse(b, nil)
	return &resp, nil
}

// DeleteBot removes a persona. Its activity log rows go with it via ON DELETE
// CASCADE; the user account it posted as is left alone, since its posts remain.
func (s *BotService) DeleteBot(ctx context.Context, id uuid.UUID) error {
	if _, err := s.store.GetBot(ctx, id); err != nil {
		if errors.Is(err, errors.ErrNotFound) {
			return errors.NewNotFoundError("bot")
		}
		return err
	}
	if err := s.store.DeleteBot(ctx, id); err != nil {
		logger.LogError(ctx, err, "bot: failed to delete persona", "bot_id", id)
		return err
	}
	logger.Info(ctx, "bot: persona deleted", "bot_id", id)
	return nil
}

// RequestRun asks for an immediate post from one persona, out of band from the
// interval. It takes effect when the bot next fetches its plan, so the operator is
// told the request was recorded rather than that a post was made.
func (s *BotService) RequestRun(ctx context.Context, id uuid.UUID) (*dto.BotResponse, error) {
	b, err := s.store.RequestBotRun(ctx, id)
	if err != nil {
		if errors.Is(err, errors.ErrNotFound) {
			return nil, errors.NewNotFoundError("bot")
		}
		logger.LogError(ctx, err, "bot: failed to request a run", "bot_id", id)
		return nil, err
	}
	logger.Info(ctx, "bot: run requested", "bot_id", id, "username", b.Username)
	resp := toBotResponse(b, nil)
	return &resp, nil
}

// ─── Config ──────────────────────────────────────────────────────────────────

// GetConfig returns the current runtime configuration.
func (s *BotService) GetConfig(ctx context.Context) (*dto.ConfigResponse, error) {
	cfg, err := s.store.GetConfig(ctx)
	if err != nil {
		logger.LogError(ctx, err, "bot: failed to load config")
		return nil, err
	}
	resp := toConfigResponse(cfg)
	return &resp, nil
}

// UpdateConfig applies a partial configuration update. Every bound mirrors a CHECK
// constraint so a bad value fails with a readable message instead of a constraint
// violation.
func (s *BotService) UpdateConfig(ctx context.Context, req *dto.UpdateConfigRequest, adminID uuid.UUID) (*dto.ConfigResponse, error) {
	update := entity.ConfigUpdate{Paused: req.Paused, UpdatedBy: &adminID}

	if req.PostIntervalSeconds != nil {
		interval := time.Duration(*req.PostIntervalSeconds) * time.Second
		if interval < entity.MinPostInterval {
			return nil, errors.NewValidationError("post_interval_seconds",
				fmt.Sprintf("must be at least %d", int64(entity.MinPostInterval/time.Second)))
		}
		update.PostInterval = &interval
	}

	if req.Accounts != nil {
		accounts := int(*req.Accounts)
		if accounts < entity.MinAccounts || accounts > entity.MaxAccounts {
			return nil, errors.NewValidationError("accounts",
				fmt.Sprintf("must be between %d and %d", entity.MinAccounts, entity.MaxAccounts))
		}
		update.Accounts = &accounts
	}

	// An empty array reaches here indistinguishable from an omitted field, and an
	// empty fallback chain would stall the bot, so only a non-empty list is applied.
	for _, m := range req.Models {
		if m == "" {
			return nil, errors.NewValidationError("models", "cannot contain an empty model id")
		}
	}
	update.Models = req.Models

	cfg, err := s.store.UpdateConfig(ctx, update)
	if err != nil {
		logger.LogError(ctx, err, "bot: failed to update config", "admin_id", adminID)
		return nil, err
	}

	logger.Info(ctx, "bot: config updated",
		"admin_id", adminID,
		"post_interval", cfg.PostInterval.String(),
		"accounts", cfg.Accounts,
		"models", cfg.Models,
		"paused", cfg.Paused,
	)
	resp := toConfigResponse(cfg)
	return &resp, nil
}

// SetPaused flips the global kill switch. Each persona's Enabled flag is untouched,
// so resuming restores exactly the previous selection.
func (s *BotService) SetPaused(ctx context.Context, paused bool, adminID uuid.UUID) (*dto.ConfigResponse, error) {
	return s.UpdateConfig(ctx, &dto.UpdateConfigRequest{Paused: &paused}, adminID)
}

// ─── Topics ──────────────────────────────────────────────────────────────────

// ListTopics returns the whole topic pool, retired entries included.
func (s *BotService) ListTopics(ctx context.Context) (*dto.ListTopicsResponse, error) {
	topics, err := s.store.ListTopics(ctx)
	if err != nil {
		logger.LogError(ctx, err, "bot: failed to list topics")
		return nil, err
	}
	data := make([]dto.TopicResponse, 0, len(topics))
	for _, t := range topics {
		data = append(data, toTopicResponse(t))
	}
	return &dto.ListTopicsResponse{Data: data}, nil
}

// CreateTopic adds a subject to the pool. A duplicate is a conflict, because a
// repeated subject would skew the random draw toward it.
func (s *BotService) CreateTopic(ctx context.Context, req *dto.CreateTopicRequest) (*dto.TopicResponse, error) {
	if req.Content == "" {
		return nil, errors.NewValidationError("content", "is required")
	}

	// No pre-check for a duplicate: MapDBError already turns the UNIQUE violation
	// into a 409, and unlike a username there is nothing to add to it. Note that
	// the mapper builds a fresh conflict error rather than returning the ErrConflict
	// singleton, so matching on it here would never fire.
	t, err := s.store.CreateTopic(ctx, req.Content)
	if err != nil {
		logger.LogError(ctx, err, "bot: failed to create topic")
		return nil, err
	}
	logger.Info(ctx, "bot: topic created", "topic_id", t.ID)
	resp := toTopicResponse(t)
	return &resp, nil
}

// SetTopicEnabled retires or restores a subject, keeping it out of the draw without
// deleting the row the activity log refers to.
func (s *BotService) SetTopicEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*dto.TopicResponse, error) {
	t, err := s.store.SetTopicEnabled(ctx, id, enabled)
	if err != nil {
		if errors.Is(err, errors.ErrNotFound) {
			return nil, errors.NewNotFoundError("topic")
		}
		logger.LogError(ctx, err, "bot: failed to set topic state", "topic_id", id)
		return nil, err
	}
	logger.Info(ctx, "bot: topic state changed", "topic_id", id, "enabled", enabled)
	resp := toTopicResponse(t)
	return &resp, nil
}

// DeleteTopic removes a subject outright. Prefer SetTopicEnabled(false) unless the
// subject should disappear entirely.
func (s *BotService) DeleteTopic(ctx context.Context, id uuid.UUID) error {
	if err := s.store.DeleteTopic(ctx, id); err != nil {
		logger.LogError(ctx, err, "bot: failed to delete topic", "topic_id", id)
		return err
	}
	logger.Info(ctx, "bot: topic deleted", "topic_id", id)
	return nil
}

// ─── Activity log ────────────────────────────────────────────────────────────

// ListRuns returns the activity log newest first, optionally for one persona.
// Post previews are filled in from the post feature when it is wired; a preview
// failure degrades the log rather than failing the request, since the status and
// error columns are the part an operator is usually looking for.
func (s *BotService) ListRuns(ctx context.Context, filter RunFilter) (*dto.ListRunsResponse, error) {
	runs, err := s.store.ListRuns(ctx, filter.BotID, filter.Limit, filter.Offset)
	if err != nil {
		logger.LogError(ctx, err, "bot: failed to list runs")
		return nil, err
	}

	total, err := s.store.CountRuns(ctx, filter.BotID)
	if err != nil {
		logger.LogError(ctx, err, "bot: failed to count runs")
		return nil, err
	}

	previews := s.postPreviews(ctx, runs)

	data := make([]dto.RunResponse, 0, len(runs))
	for _, r := range runs {
		data = append(data, toRunResponse(r, previews))
	}
	return &dto.ListRunsResponse{
		Data:               data,
		PaginationResponse: pagination.NewPaginationResponse(total, filter.Limit, filter.Offset),
	}, nil
}

// postPreviews resolves the posts referenced by a page of runs in one batch.
// Returns nil when there is no reader wired, nothing to look up, or the lookup
// failed — every caller treats a missing preview as simply absent.
func (s *BotService) postPreviews(ctx context.Context, runs []*entity.Run) map[uuid.UUID]string {
	if s.posts == nil {
		return nil
	}

	ids := make([]uuid.UUID, 0, len(runs))
	for _, r := range runs {
		if r.PostID != nil {
			ids = append(ids, *r.PostID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	previews, err := s.posts.GetPostPreviews(ctx, ids)
	if err != nil {
		logger.Warn(ctx, "bot: failed to resolve post previews for the activity log", "error", err)
		return nil
	}
	return previews
}

// ─── Agent plane ─────────────────────────────────────────────────────────────

// GetPlan returns the desired state for the bot process: the config it runs under
// and the personas and topics it may use. Personas are already filtered to the
// enabled ones and capped at the configured account count, so the bot applies no
// selection rules of its own.
//
// An empty persona or topic pool is reported as-is rather than as an error — the
// endpoint describes state, and the bot has no useful recovery — but it is logged,
// because the result is a bot that polls and never posts.
func (s *BotService) GetPlan(ctx context.Context) (*dto.PlanResponse, error) {
	cfg, err := s.store.GetConfig(ctx)
	if err != nil {
		logger.LogError(ctx, err, "bot: failed to load config for the plan")
		return nil, err
	}

	bots, err := s.store.ListEnabledBots(ctx, int32(cfg.Accounts)) //nolint:gosec // bounded by MaxAccounts on write
	if err != nil {
		logger.LogError(ctx, err, "bot: failed to load personas for the plan")
		return nil, err
	}

	topics, err := s.store.ListEnabledTopics(ctx)
	if err != nil {
		logger.LogError(ctx, err, "bot: failed to load topics for the plan")
		return nil, err
	}

	if len(bots) == 0 || len(topics) == 0 {
		logger.Warn(ctx, "bot: plan has nothing to post with",
			"enabled_bots", len(bots),
			"enabled_topics", len(topics),
		)
	}

	planBots := make([]dto.PlanBot, 0, len(bots))
	for _, b := range bots {
		planBots = append(planBots, dto.PlanBot{
			ID:           b.ID.String(),
			Username:     b.Username,
			DisplayName:  b.DisplayName,
			Style:        b.Style,
			RunRequested: b.RunRequested(),
		})
	}

	topicContents := make([]string, 0, len(topics))
	for _, t := range topics {
		topicContents = append(topicContents, t.Content)
	}

	return &dto.PlanResponse{
		Paused:              cfg.Paused,
		PostIntervalSeconds: entity.DurationToSeconds(cfg.PostInterval),
		Models:              cfg.Models,
		Bots:                planBots,
		Topics:              topicContents,
	}, nil
}

// ReportRun records one attempt the bot made. The outcome fields are validated
// against the status here so an under-populated report fails as a validation error
// instead of a CHECK violation.
//
// The pending run-now flag is cleared only when the bot says this attempt was the
// one that honoured it. Clearing on every report would discard a request that
// arrived after the bot last fetched its plan, and that post would never happen.
func (s *BotService) ReportRun(ctx context.Context, req *dto.ReportRunRequest) error {
	botID, err := uuid.Parse(req.BotID)
	if err != nil {
		return errors.NewValidationError("bot_id", "must be a UUID")
	}

	status, ok := entity.ParseRunStatus(req.Status)
	if !ok {
		return errors.NewValidationError("status", fmt.Sprintf("must be %q or %q", entity.RunSuccess, entity.RunError))
	}

	var postID *uuid.UUID
	if req.PostID != nil {
		parsed, parseErr := uuid.Parse(*req.PostID)
		if parseErr != nil {
			return errors.NewValidationError("post_id", "must be a UUID")
		}
		postID = &parsed
	}

	run := entity.NewRun{
		BotID:     botID,
		PostID:    postID,
		ModelUsed: req.ModelUsed,
		Status:    status,
		Error:     req.Error,
	}
	if !run.OutcomeConsistent() {
		return errors.NewBadRequestError("a success must carry post_id and no error; a failure must carry error and no post_id")
	}

	if _, err = s.store.GetBot(ctx, botID); err != nil {
		if errors.Is(err, errors.ErrNotFound) {
			return errors.NewNotFoundError("bot")
		}
		return err
	}

	if _, err = s.store.CreateRun(ctx, run); err != nil {
		logger.LogError(ctx, err, "bot: failed to record a run", "bot_id", botID)
		return err
	}

	if req.HonoredRunRequest {
		if err = s.store.ClearBotRunRequest(ctx, botID); err != nil {
			// The run is already recorded, so failing here would make the bot retry a
			// post it already made. Log and move on; the flag clears on the next report.
			logger.LogError(ctx, err, "bot: failed to clear the run request", "bot_id", botID)
		}
	}

	logger.Info(ctx, "bot: run recorded", "bot_id", botID, "status", status)
	return nil
}

// LinkIdentity records which user account a persona posts as, reported by the bot
// once it has registered or logged the account in.
func (s *BotService) LinkIdentity(ctx context.Context, botID uuid.UUID, req *dto.LinkIdentityRequest) error {
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		return errors.NewValidationError("user_id", "must be a UUID")
	}

	if _, err = s.store.GetBot(ctx, botID); err != nil {
		if errors.Is(err, errors.ErrNotFound) {
			return errors.NewNotFoundError("bot")
		}
		return err
	}

	if err = s.store.LinkBotUser(ctx, botID, userID); err != nil {
		logger.LogError(ctx, err, "bot: failed to link the persona account", "bot_id", botID, "user_id", userID)
		return err
	}

	logger.Info(ctx, "bot: persona linked to user", "bot_id", botID, "user_id", userID)
	return nil
}

// ─── Private helpers ─────────────────────────────────────────────────────────

func toBotResponse(b *entity.Bot, stats *entity.RunStats) dto.BotResponse {
	resp := dto.BotResponse{
		ID:             b.ID.String(),
		Username:       b.Username,
		DisplayName:    b.DisplayName,
		Style:          b.Style,
		Enabled:        b.Enabled,
		UserID:         formatUUID(b.UserID),
		RunRequestedAt: formatTime(b.RunRequestedAt),
		CreatedAt:      b.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:      b.UpdatedAt.UTC().Format(timeFormat),
	}
	if stats != nil {
		resp.LastSuccessAt = formatTime(stats.LastSuccessAt)
		resp.LastErrorAt = formatTime(stats.LastErrorAt)
		resp.SuccessesLast24h = stats.SuccessesLast24h
		resp.ErrorsLast24h = stats.ErrorsLast24h
	}
	return resp
}

func toConfigResponse(c *entity.Config) dto.ConfigResponse {
	return dto.ConfigResponse{
		PostIntervalSeconds: entity.DurationToSeconds(c.PostInterval),
		Accounts:            int32(c.Accounts), //nolint:gosec // bounded by MaxAccounts on write
		Models:              c.Models,
		Paused:              c.Paused,
		UpdatedBy:           formatUUID(c.UpdatedBy),
		UpdatedAt:           c.UpdatedAt.UTC().Format(timeFormat),
	}
}

func toTopicResponse(t *entity.Topic) dto.TopicResponse {
	return dto.TopicResponse{
		ID:        t.ID.String(),
		Content:   t.Content,
		Enabled:   t.Enabled,
		CreatedAt: t.CreatedAt.UTC().Format(timeFormat),
	}
}

func toRunResponse(r *entity.Run, previews map[uuid.UUID]string) dto.RunResponse {
	resp := dto.RunResponse{
		ID:        r.ID.String(),
		BotID:     r.BotID.String(),
		PostID:    formatUUID(r.PostID),
		ModelUsed: r.ModelUsed,
		Status:    r.Status.String(),
		Error:     r.Error,
		CreatedAt: r.CreatedAt.UTC().Format(timeFormat),
	}
	if r.PostID != nil {
		if content, ok := previews[*r.PostID]; ok {
			preview := truncate(content, previewLength)
			resp.PostPreview = &preview
		}
	}
	return resp
}

func formatUUID(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func formatTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(timeFormat)
	return &s
}

// truncate shortens s to at most limit runes, appending an ellipsis when it cuts.
// Rune-aware because post content is routinely non-ASCII.
func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}
