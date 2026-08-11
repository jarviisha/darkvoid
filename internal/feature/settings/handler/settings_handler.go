package handler

import (
	"net/http"

	"github.com/jarviisha/darkvoid/internal/feature/settings/dto"
	httputil "github.com/jarviisha/darkvoid/internal/http"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

// SettingsHandler serves the /admin/settings/* operator surface.
type SettingsHandler struct {
	settingsService settingsService
}

func NewSettingsHandler(settingsService settingsService) *SettingsHandler {
	return &SettingsHandler{settingsService: settingsService}
}

// GetFeedSettings godoc
//
//	@Summary		Get feed runtime settings
//	@Description	Returns the feed knobs that can be changed while the API is running: prepared-timeline serving and its rollout, fanout, and the three local ranking weights. Fanout worker count and queue size are not here — they size a goroutine pool at construction and still require a restart, so they stay in the environment.
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	dto.FeedSettingsResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@Failure		500	{object}	errors.ErrorResponse
//	@ID				adminGetFeedSettings
//	@Router			/admin/settings/feed [get]
//	@Security		BearerAuth
func (h *SettingsHandler) GetFeedSettings(w http.ResponseWriter, r *http.Request) {
	resp, err := h.settingsService.GetFeedSettings(r.Context())
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// UpdateFeedSettings godoc
//
//	@Summary		Update feed runtime settings
//	@Description	Partial update — an omitted field keeps its stored value, and a body naming no field at all is a 400 rather than an edit that changes nothing. Takes effect on this instance immediately and on any other instance within one refresh interval, without a restart.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.UpdateFeedSettingsRequest	true	"Fields to change"
//	@Success		200		{object}	dto.FeedSettingsResponse
//	@Failure		400		{object}	errors.ErrorResponse
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@Failure		500		{object}	errors.ErrorResponse
//	@ID				adminUpdateFeedSettings
//	@Router			/admin/settings/feed [patch]
//	@Security		BearerAuth
func (h *SettingsHandler) UpdateFeedSettings(w http.ResponseWriter, r *http.Request) {
	adminID := httputil.GetUserID(r.Context())
	if adminID == nil {
		errors.WriteJSON(w, errors.NewUnauthorizedError("user not authenticated"))
		return
	}

	var req dto.UpdateFeedSettingsRequest
	if err := httputil.DecodeJSON(w, r, &req); err != nil {
		errors.WriteJSON(w, err)
		return
	}

	resp, err := h.settingsService.UpdateFeedSettings(r.Context(), &req, *adminID)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}
