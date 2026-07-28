package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/bot/dto"
	"github.com/jarviisha/darkvoid/internal/feature/bot/service"
	httputil "github.com/jarviisha/darkvoid/internal/http"
	"github.com/jarviisha/darkvoid/internal/pagination"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// BotHandler serves /admin/bots/* for operators and /bot/* for the bot process.
type BotHandler struct {
	botService botService
}

func NewBotHandler(botService botService) *BotHandler {
	return &BotHandler{botService: botService}
}

// ─── Personas (admin) ────────────────────────────────────────────────────────

// ListBots godoc
//
//	@Summary		List bot personas
//	@Description	Returns every content-bot persona with a summary of its recent activity. The error text behind last_error_at is in the activity log, not here.
//	@Tags			admin,bot
//	@Produce		json
//	@Success		200	{object}	dto.ListBotsResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@Failure		500	{object}	errors.ErrorResponse
//	@ID				adminListBots
//	@Router			/admin/bots [get]
//	@Security		BearerAuth
func (h *BotHandler) ListBots(w http.ResponseWriter, r *http.Request) {
	resp, err := h.botService.ListBots(r.Context())
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// CreateBot godoc
//
//	@Summary		Add a bot persona
//	@Description	Creates a persona. The bot registers the account on its next tick, so the username must satisfy the user-service rule.
//	@Tags			admin,bot
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.CreateBotRequest	true	"Persona"
//	@Success		201		{object}	dto.BotResponse
//	@Failure		400		{object}	errors.ErrorResponse
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@Failure		409		{object}	errors.ErrorResponse	"Username already taken by another persona"
//	@ID				adminCreateBot
//	@Router			/admin/bots [post]
//	@Security		BearerAuth
func (h *BotHandler) CreateBot(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateBotRequest
	if !decodeBody(w, r, &req, "create bot") {
		return
	}

	resp, err := h.botService.CreateBot(r.Context(), &req)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, resp)
}

// UpdateBot godoc
//
//	@Summary		Update a bot persona
//	@Description	Partial update — an omitted field keeps its stored value. The username cannot be changed, since it is the registered account the persona posts as.
//	@Tags			admin,bot
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Persona UUID"
//	@Param			body	body		dto.UpdateBotRequest	true	"Fields to change"
//	@Success		200		{object}	dto.BotResponse
//	@Failure		400		{object}	errors.ErrorResponse
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@Failure		404		{object}	errors.ErrorResponse
//	@ID				adminUpdateBot
//	@Router			/admin/bots/{id} [patch]
//	@Security		BearerAuth
func (h *BotHandler) UpdateBot(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	var req dto.UpdateBotRequest
	if !decodeBody(w, r, &req, "update bot") {
		return
	}

	resp, err := h.botService.UpdateBot(r.Context(), id, &req)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// DeleteBot godoc
//
//	@Summary		Delete a bot persona
//	@Description	Removes the persona and its activity log. The user account it posted as is left alone, since its posts remain.
//	@Tags			admin,bot
//	@Produce		json
//	@Param			id	path	string	true	"Persona UUID"
//	@Success		204	"No Content"
//	@Failure		400	{object}	errors.ErrorResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@Failure		404	{object}	errors.ErrorResponse
//	@ID				adminDeleteBot
//	@Router			/admin/bots/{id} [delete]
//	@Security		BearerAuth
func (h *BotHandler) DeleteBot(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	if err := h.botService.DeleteBot(r.Context(), id); err != nil {
		errors.WriteJSON(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RequestRun godoc
//
//	@Summary		Ask a persona to post now
//	@Description	Records a request for an immediate post. It takes effect when the bot next fetches its plan, so a 200 means the request was recorded — not that a post was made. A disabled persona is rejected, because the plan carries only enabled ones and the request could never fire.
//	@Tags			admin,bot
//	@Produce		json
//	@Param			id	path		string	true	"Persona UUID"
//	@Success		200	{object}	dto.BotResponse
//	@Failure		400	{object}	errors.ErrorResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@Failure		404	{object}	errors.ErrorResponse
//	@Failure		409	{object}	errors.ErrorResponse	"Persona is disabled"
//	@ID				adminRequestBotRun
//	@Router			/admin/bots/{id}/run-now [post]
//	@Security		BearerAuth
func (h *BotHandler) RequestRun(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	resp, err := h.botService.RequestRun(r.Context(), id)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// ─── Config (admin) ──────────────────────────────────────────────────────────

// GetConfig godoc
//
//	@Summary		Get bot configuration
//	@Description	Returns the runtime configuration the bot reads on every tick.
//	@Tags			admin,bot
//	@Produce		json
//	@Success		200	{object}	dto.ConfigResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@Failure		500	{object}	errors.ErrorResponse
//	@ID				adminGetBotConfig
//	@Router			/admin/bots/config [get]
//	@Security		BearerAuth
func (h *BotHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	resp, err := h.botService.GetConfig(r.Context())
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// UpdateConfig godoc
//
//	@Summary		Update bot configuration
//	@Description	Partial update — an omitted field keeps its stored value, and an empty models array counts as omitted. Takes effect on the bot's next tick, without a restart.
//	@Tags			admin,bot
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.UpdateConfigRequest	true	"Fields to change"
//	@Success		200		{object}	dto.ConfigResponse
//	@Failure		400		{object}	errors.ErrorResponse
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@ID				adminUpdateBotConfig
//	@Router			/admin/bots/config [put]
//	@Security		BearerAuth
func (h *BotHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	adminID := httputil.GetUserID(r.Context())
	if adminID == nil {
		errors.WriteJSON(w, errors.NewUnauthorizedError("user not authenticated"))
		return
	}

	var req dto.UpdateConfigRequest
	if !decodeBody(w, r, &req, "update bot config") {
		return
	}

	resp, err := h.botService.UpdateConfig(r.Context(), &req, *adminID)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// Pause godoc
//
//	@Summary		Pause the bot
//	@Description	Global kill switch. Each persona's enabled flag is left alone, so resuming restores exactly the previous selection.
//	@Tags			admin,bot
//	@Produce		json
//	@Success		200	{object}	dto.ConfigResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@ID				adminPauseBot
//	@Router			/admin/bots/pause [post]
//	@Security		BearerAuth
func (h *BotHandler) Pause(w http.ResponseWriter, r *http.Request) {
	h.setPaused(w, r, true)
}

// Resume godoc
//
//	@Summary		Resume the bot
//	@Description	Clears the global pause. The personas that were enabled before resume posting.
//	@Tags			admin,bot
//	@Produce		json
//	@Success		200	{object}	dto.ConfigResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@ID				adminResumeBot
//	@Router			/admin/bots/resume [post]
//	@Security		BearerAuth
func (h *BotHandler) Resume(w http.ResponseWriter, r *http.Request) {
	h.setPaused(w, r, false)
}

func (h *BotHandler) setPaused(w http.ResponseWriter, r *http.Request, paused bool) {
	adminID := httputil.GetUserID(r.Context())
	if adminID == nil {
		errors.WriteJSON(w, errors.NewUnauthorizedError("user not authenticated"))
		return
	}

	resp, err := h.botService.SetPaused(r.Context(), paused, *adminID)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// ─── Topics (admin) ──────────────────────────────────────────────────────────

// ListTopics godoc
//
//	@Summary		List bot topics
//	@Description	Returns the whole subject pool, retired entries included, so they can be restored.
//	@Tags			admin,bot
//	@Produce		json
//	@Success		200	{object}	dto.ListTopicsResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@Failure		500	{object}	errors.ErrorResponse
//	@ID				adminListBotTopics
//	@Router			/admin/bots/topics [get]
//	@Security		BearerAuth
func (h *BotHandler) ListTopics(w http.ResponseWriter, r *http.Request) {
	resp, err := h.botService.ListTopics(r.Context())
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// CreateTopic godoc
//
//	@Summary		Add a bot topic
//	@Description	Adds a subject to the pool. Duplicates are rejected, because a repeated subject would skew the random draw toward it.
//	@Tags			admin,bot
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.CreateTopicRequest	true	"Topic"
//	@Success		201		{object}	dto.TopicResponse
//	@Failure		400		{object}	errors.ErrorResponse
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@Failure		409		{object}	errors.ErrorResponse	"Topic already in the pool"
//	@ID				adminCreateBotTopic
//	@Router			/admin/bots/topics [post]
//	@Security		BearerAuth
func (h *BotHandler) CreateTopic(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateTopicRequest
	if !decodeBody(w, r, &req, "create bot topic") {
		return
	}

	resp, err := h.botService.CreateTopic(r.Context(), &req)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, resp)
}

// SetTopicEnabled godoc
//
//	@Summary		Retire or restore a bot topic
//	@Description	Keeps a subject out of the draw without deleting the row the activity log refers to.
//	@Tags			admin,bot
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string						true	"Topic UUID"
//	@Param			body	body		dto.SetTopicEnabledRequest	true	"Desired state"
//	@Success		200		{object}	dto.TopicResponse
//	@Failure		400		{object}	errors.ErrorResponse
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@Failure		404		{object}	errors.ErrorResponse
//	@ID				adminSetBotTopicEnabled
//	@Router			/admin/bots/topics/{id} [patch]
//	@Security		BearerAuth
func (h *BotHandler) SetTopicEnabled(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	var req dto.SetTopicEnabledRequest
	if !decodeBody(w, r, &req, "set bot topic state") {
		return
	}

	resp, err := h.botService.SetTopicEnabled(r.Context(), id, req.Enabled)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// DeleteTopic godoc
//
//	@Summary		Delete a bot topic
//	@Description	Removes a subject outright. Prefer retiring it unless it should disappear entirely.
//	@Tags			admin,bot
//	@Produce		json
//	@Param			id	path	string	true	"Topic UUID"
//	@Success		204	"No Content"
//	@Failure		400	{object}	errors.ErrorResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@ID				adminDeleteBotTopic
//	@Router			/admin/bots/topics/{id} [delete]
//	@Security		BearerAuth
func (h *BotHandler) DeleteTopic(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	if err := h.botService.DeleteTopic(r.Context(), id); err != nil {
		errors.WriteJSON(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Activity log (admin) ────────────────────────────────────────────────────

// ListRuns godoc
//
//	@Summary		Read the bot activity log
//	@Description	Returns reported post attempts, newest first. Before this log existed the record lived only in the bot host's journal.
//	@Tags			admin,bot
//	@Produce		json
//	@Param			limit	query		int		false	"Max items per page (default 20)"
//	@Param			offset	query		int		false	"Number of items to skip"
//	@Param			bot_id	query		string	false	"Limit to one persona (omit for all)"
//	@Success		200		{object}	dto.ListRunsResponse
//	@Failure		400		{object}	errors.ErrorResponse
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@ID				adminListBotRuns
//	@Router			/admin/bots/runs [get]
//	@Security		BearerAuth
func (h *BotHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	filter := service.RunFilter{PaginationRequest: pagination.ParseQuery(r)}

	// An unparseable bot_id is rejected rather than ignored: silently returning the
	// unfiltered log would read as "this persona did all of that".
	if raw := r.URL.Query().Get("bot_id"); raw != "" {
		botID, err := uuid.Parse(raw)
		if err != nil {
			errors.WriteJSON(w, errors.NewBadRequestError("invalid bot_id — must be a UUID"))
			return
		}
		filter.BotID = &botID
	}

	resp, err := h.botService.ListRuns(r.Context(), filter)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// ─── Agent plane ─────────────────────────────────────────────────────────────

// GetPlan godoc
//
//	@Summary		Get the bot's desired state
//	@Description	Polled by the bot process on every tick. Personas are already filtered to the enabled ones and capped at the configured account count, so the bot applies no selection rules of its own. Requires the bot role — an admin token gets 403 here.
//	@Tags			admin,bot
//	@Produce		json
//	@Success		200	{object}	dto.PlanResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@Failure		500	{object}	errors.ErrorResponse
//	@ID				botGetPlan
//	@Router			/bot/plan [get]
//	@Security		BearerAuth
func (h *BotHandler) GetPlan(w http.ResponseWriter, r *http.Request) {
	resp, err := h.botService.GetPlan(r.Context())
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// ReportRun godoc
//
//	@Summary		Report a post attempt
//	@Description	A success must carry post_id and no error; a failure must carry error and no post_id. Set honored_run_request only for the attempt that answered an operator's run-now, since that is what clears the pending flag. Requires the bot role.
//	@Tags			admin,bot
//	@Accept			json
//	@Produce		json
//	@Param			body	body	dto.ReportRunRequest	true	"Attempt"
//	@Success		204		"No Content"
//	@Failure		400		{object}	errors.ErrorResponse
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@Failure		404		{object}	errors.ErrorResponse	"Unknown persona"
//	@ID				botReportRun
//	@Router			/bot/runs [post]
//	@Security		BearerAuth
func (h *BotHandler) ReportRun(w http.ResponseWriter, r *http.Request) {
	var req dto.ReportRunRequest
	if !decodeBody(w, r, &req, "report bot run") {
		return
	}

	if err := h.botService.ReportRun(r.Context(), &req); err != nil {
		errors.WriteJSON(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// LinkIdentity godoc
//
//	@Summary		Link a persona to its user account
//	@Description	Reported by the bot once it has registered or logged the persona's account in, so the admin view can show which user it posts as. Requires the bot role.
//	@Tags			admin,bot
//	@Accept			json
//	@Produce		json
//	@Param			id		path	string					true	"Persona UUID"
//	@Param			body	body	dto.LinkIdentityRequest	true	"Account"
//	@Success		204		"No Content"
//	@Failure		400		{object}	errors.ErrorResponse
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@Failure		404		{object}	errors.ErrorResponse
//	@ID				botLinkIdentity
//	@Router			/bot/{id}/identity [post]
//	@Security		BearerAuth
func (h *BotHandler) LinkIdentity(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	var req dto.LinkIdentityRequest
	if !decodeBody(w, r, &req, "link bot identity") {
		return
	}

	if err := h.botService.LinkIdentity(r.Context(), id, &req); err != nil {
		errors.WriteJSON(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// parseID reads the {id} path parameter, which addresses a persona on the bot
// routes and a topic on the topic routes.
func parseID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, errors.NewBadRequestError("invalid id — must be a UUID")
	}
	return id, nil
}

// decodeBody decodes a JSON request body, writing the error response itself and
// reporting whether the caller should continue.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, what string) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		logger.Warn(r.Context(), "bot: invalid request body", "operation", what, "error", err)
		errors.WriteJSON(w, errors.NewBadRequestError("invalid request body"))
		return false
	}
	return true
}
