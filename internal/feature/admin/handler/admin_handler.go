package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/admin/dto"
	"github.com/jarviisha/darkvoid/internal/feature/admin/service"
	httputil "github.com/jarviisha/darkvoid/internal/http"
	"github.com/jarviisha/darkvoid/internal/pagination"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// AdminHandler handles all /admin/* HTTP endpoints.
type AdminHandler struct {
	adminService adminService
}

// NewAdminHandler creates an AdminHandler with the given service dependency.
func NewAdminHandler(adminService adminService) *AdminHandler {
	return &AdminHandler{adminService: adminService}
}

// ─── User Management ─────────────────────────────────────────────────────────

// ListUsers godoc
//
//	@Summary		List all users
//	@Description	Returns a paginated list of all users. Supports filtering by status and search query.
//	@Tags			admin
//	@Produce		json
//	@Param			limit		query		int		false	"Max items per page (default 20)"
//	@Param			offset		query		int		false	"Number of items to skip"
//	@Param			q			query		string	false	"Search by username, email, or display name"
//	@Param			is_active	query		bool	false	"Filter by active status (omit for all)"
//	@Success		200			{object}	dto.AdminListUsersResponse
//	@Failure		401			{object}	errors.ErrorResponse
//	@Failure		403			{object}	errors.ErrorResponse
//	@Failure		500			{object}	errors.ErrorResponse
//	@ID				adminListUsers
//	@Router			/admin/users [get]
//	@Security		BearerAuth
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pag := pagination.ParseQuery(r)

	filter := service.AdminListUsersFilter{
		PaginationRequest: pag,
	}

	if q := r.URL.Query().Get("q"); q != "" {
		filter.Query = &q
	}

	if raw := r.URL.Query().Get("is_active"); raw != "" {
		active := raw == "true"
		filter.IsActive = &active
	}

	resp, err := h.adminService.ListUsers(ctx, filter)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// GetUser godoc
//
//	@Summary		Get user details
//	@Description	Returns the admin view of a single user including email and active status.
//	@Tags			admin
//	@Produce		json
//	@Param			id	path		string	true	"User UUID"
//	@Success		200	{object}	dto.AdminUserResponse
//	@Failure		400	{object}	errors.ErrorResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@Failure		404	{object}	errors.ErrorResponse
//	@ID				adminGetUser
//	@Router			/admin/users/{id} [get]
//	@Security		BearerAuth
func (h *AdminHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, err := parseUserID(r)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	resp, err := h.adminService.GetUser(ctx, userID)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// SetUserStatus godoc
//
//	@Summary		Activate or deactivate a user
//	@Description	Sets the active status of a user account. Deactivated users cannot log in.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			id		path	string							true	"User UUID"
//	@Param			body	body	dto.AdminSetUserStatusRequest	true	"Status payload"
//	@Success		204		"No Content"
//	@Failure		400		{object}	errors.ErrorResponse
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@Failure		404		{object}	errors.ErrorResponse
//	@ID				adminSetUserStatus
//	@Router			/admin/users/{id}/status [patch]
//	@Security		BearerAuth
func (h *AdminHandler) SetUserStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	adminID := httputil.GetUserID(ctx)
	if adminID == nil {
		errors.WriteJSON(w, errors.NewUnauthorizedError("user not authenticated"))
		return
	}

	targetID, err := parseUserID(r)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	var req dto.AdminSetUserStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Warn(ctx, "admin: invalid status request body", "error", err)
		errors.WriteJSON(w, errors.NewBadRequestError("invalid request body"))
		return
	}

	if err := h.adminService.SetUserActive(ctx, targetID, req.IsActive, *adminID); err != nil {
		errors.WriteJSON(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ─── Role Management ─────────────────────────────────────────────────────────

// ListRoles godoc
//
//	@Summary		List assignable roles
//	@Description	Returns the fixed set of roles that can be assigned to users. Roles are defined by the application, not created at runtime.
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	dto.ListRolesResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@ID				adminListRoles
//	@Router			/admin/roles [get]
//	@Security		BearerAuth
func (h *AdminHandler) ListRoles(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, h.adminService.ListRoles())
}

// GetUserRoles godoc
//
//	@Summary		Get roles for a user
//	@Description	Returns all roles currently assigned to the specified user.
//	@Tags			admin
//	@Produce		json
//	@Param			id	path		string	true	"User UUID"
//	@Success		200	{object}	dto.UserRolesResponse
//	@Failure		400	{object}	errors.ErrorResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@Failure		404	{object}	errors.ErrorResponse
//	@ID				adminGetUserRoles
//	@Router			/admin/users/{id}/roles [get]
//	@Security		BearerAuth
func (h *AdminHandler) GetUserRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, err := parseUserID(r)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	resp, err := h.adminService.GetUserRoles(ctx, userID)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// AssignRole godoc
//
//	@Summary		Assign a role to a user
//	@Description	Grants a role to the specified user.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			id		path	string					true	"User UUID"
//	@Param			body	body	dto.AssignRoleRequest	true	"Role to assign"
//	@Success		204		"No Content"
//	@Failure		400		{object}	errors.ErrorResponse	"Unknown role name"
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@Failure		404		{object}	errors.ErrorResponse
//	@ID				adminAssignRole
//	@Router			/admin/users/{id}/roles [post]
//	@Security		BearerAuth
func (h *AdminHandler) AssignRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	adminID := httputil.GetUserID(ctx)
	if adminID == nil {
		errors.WriteJSON(w, errors.NewUnauthorizedError("user not authenticated"))
		return
	}

	userID, err := parseUserID(r)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	var req dto.AssignRoleRequest
	if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Warn(ctx, "admin: invalid assign role request body", "error", err)
		errors.WriteJSON(w, errors.NewBadRequestError("invalid request body"))
		return
	}

	if err := h.adminService.AssignRole(ctx, userID, req.Role, *adminID); err != nil {
		errors.WriteJSON(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoveRole godoc
//
//	@Summary		Remove a role from a user
//	@Description	Revokes a role from the specified user.
//	@Tags			admin
//	@Produce		json
//	@Param			id		path	string	true	"User UUID"
//	@Param			role	path	string	true	"Role name"	Enums(admin, moderator)
//	@Success		204		"No Content"
//	@Failure		400		{object}	errors.ErrorResponse	"Unknown role name"
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@ID				adminRemoveRole
//	@Router			/admin/users/{id}/roles/{role} [delete]
//	@Security		BearerAuth
func (h *AdminHandler) RemoveRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	adminID := httputil.GetUserID(ctx)
	if adminID == nil {
		errors.WriteJSON(w, errors.NewUnauthorizedError("user not authenticated"))
		return
	}

	userID, err := parseUserID(r)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	if err := h.adminService.RemoveRole(ctx, userID, chi.URLParam(r, "role"), *adminID); err != nil {
		errors.WriteJSON(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ─── Stats ────────────────────────────────────────────────────────────────────

// GetStats godoc
//
//	@Summary		Get platform statistics
//	@Description	Returns basic counts for users and roles.
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	dto.AdminStatsResponse
//	@Failure		401	{object}	errors.ErrorResponse
//	@Failure		403	{object}	errors.ErrorResponse
//	@Failure		500	{object}	errors.ErrorResponse
//	@ID				adminGetStats
//	@Router			/admin/stats [get]
//	@Security		BearerAuth
func (h *AdminHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	resp, err := h.adminService.GetStats(ctx)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// ─── Notifications ────────────────────────────────────────────────────────────

// SendNotificationToUser godoc
//
//	@Summary		Send notification to a user
//	@Description	Sends a system announcement notification to a specific user.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			id		path	string								true	"Target user UUID"
//	@Param			body	body	dto.AdminSendNotificationRequest	true	"Notification payload"
//	@Success		204		"No Content"
//	@Failure		400		{object}	errors.ErrorResponse
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@Failure		404		{object}	errors.ErrorResponse
//	@ID				adminSendNotificationToUser
//	@Router			/admin/notifications/users/{id} [post]
//	@Security		BearerAuth
func (h *AdminHandler) SendNotificationToUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	adminID := httputil.GetUserID(ctx)
	if adminID == nil {
		errors.WriteJSON(w, errors.NewUnauthorizedError("user not authenticated"))
		return
	}

	targetID, err := parseUserID(r)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	var req dto.AdminSendNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Warn(ctx, "admin: invalid send notification request body", "error", err)
		errors.WriteJSON(w, errors.NewBadRequestError("invalid request body"))
		return
	}

	if err := h.adminService.SendNotificationToUser(ctx, *adminID, targetID, &req); err != nil {
		errors.WriteJSON(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// BroadcastNotification godoc
//
//	@Summary		Broadcast notification to all users
//	@Description	Sends a system announcement notification to all active users.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.AdminSendNotificationRequest	true	"Notification payload"
//	@Success		200		{object}	dto.AdminBroadcastNotificationResponse
//	@Failure		400		{object}	errors.ErrorResponse
//	@Failure		401		{object}	errors.ErrorResponse
//	@Failure		403		{object}	errors.ErrorResponse
//	@ID				adminBroadcastNotification
//	@Router			/admin/notifications/broadcast [post]
//	@Security		BearerAuth
func (h *AdminHandler) BroadcastNotification(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	adminID := httputil.GetUserID(ctx)
	if adminID == nil {
		errors.WriteJSON(w, errors.NewUnauthorizedError("user not authenticated"))
		return
	}

	var req dto.AdminSendNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Warn(ctx, "admin: invalid broadcast notification request body", "error", err)
		errors.WriteJSON(w, errors.NewBadRequestError("invalid request body"))
		return
	}

	resp, err := h.adminService.BroadcastNotification(ctx, *adminID, &req)
	if err != nil {
		errors.WriteJSON(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// parseUserID reads the {id} path parameter every /admin route uses to address a
// user. Roles are addressed by name, not UUID, so this is the only ID in the
// admin surface.
func parseUserID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, errors.NewBadRequestError("invalid id — must be a UUID")
	}
	return id, nil
}
