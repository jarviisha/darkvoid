package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/admin/dto"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/internal/pagination"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

// AdminService implements admin operations across users and roles.
type AdminService struct {
	userStore    userStore
	roleStore    roleStore
	storage      storage.Storage
	notifEmitter notifEmitter // optional, nil = notifications disabled
}

// NewAdminService creates an AdminService with the required dependencies.
func NewAdminService(userStore userStore, roleStore roleStore, store storage.Storage) *AdminService {
	return &AdminService{
		userStore: userStore,
		roleStore: roleStore,
		storage:   store,
	}
}

// WithNotificationEmitter attaches a notification emitter. Called at wire-up time.
func (s *AdminService) WithNotificationEmitter(e notifEmitter) {
	s.notifEmitter = e
}

// ─── User Management ─────────────────────────────────────────────────────────

// ListUsers returns a paginated list of all users matching the given filter.
func (s *AdminService) ListUsers(ctx context.Context, filter AdminListUsersFilter) (*dto.AdminListUsersResponse, error) {
	users, err := s.userStore.AdminListUsers(ctx, filter)
	if err != nil {
		logger.LogError(ctx, err, "admin: failed to list users")
		return nil, errors.NewInternalError(err)
	}

	total, err := s.userStore.AdminCountUsers(ctx, filter)
	if err != nil {
		logger.LogError(ctx, err, "admin: failed to count users")
		return nil, errors.NewInternalError(err)
	}

	data := make([]dto.AdminUserResponse, 0, len(users))
	for _, u := range users {
		data = append(data, toAdminUserResponse(u, s.storage))
	}

	return &dto.AdminListUsersResponse{
		Data:               data,
		PaginationResponse: pagination.NewPaginationResponse(total, filter.Limit, filter.Offset),
	}, nil
}

// GetUser returns the admin view of a single user.
func (s *AdminService) GetUser(ctx context.Context, userID uuid.UUID) (*dto.AdminUserResponse, error) {
	u, err := s.userStore.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	resp := toAdminUserResponse(u, s.storage)
	return &resp, nil
}

// SetUserActive activates or deactivates a user account.
func (s *AdminService) SetUserActive(ctx context.Context, targetUserID uuid.UUID, isActive bool, adminID uuid.UUID) error {
	if err := s.userStore.AdminSetUserActive(ctx, targetUserID, isActive, adminID); err != nil {
		logger.LogError(ctx, err, "admin: failed to set user active status",
			"target_user_id", targetUserID,
			"is_active", isActive,
		)
		return fmt.Errorf("set user active %s: %w", targetUserID, err)
	}
	logger.Info(ctx, "admin: user status updated",
		"target_user_id", targetUserID,
		"is_active", isActive,
		"admin_id", adminID,
	)
	return nil
}

// ─── Role Management ─────────────────────────────────────────────────────────

// ListRoles returns the roles that can be assigned. The set is fixed at compile
// time (entity.AllRoles) and mirrored by a CHECK constraint, so this never
// touches the DB and cannot fail — it exists to document the valid values for
// AssignRole.
func (s *AdminService) ListRoles() *dto.ListRolesResponse {
	data := make([]dto.RoleResponse, 0, len(entity.AllRoles))
	for _, r := range entity.AllRoles {
		data = append(data, dto.RoleResponse{
			Name:        r.String(),
			Description: r.Description(),
		})
	}
	return &dto.ListRolesResponse{Data: data}
}

// GetUserRoles returns the roles held by a user, each with its audit trail.
func (s *AdminService) GetUserRoles(ctx context.Context, userID uuid.UUID) (*dto.UserRolesResponse, error) {
	assignments, err := s.roleStore.GetUserRoles(ctx, userID)
	if err != nil {
		logger.LogError(ctx, err, "admin: failed to get user roles", "user_id", userID)
		return nil, errors.NewInternalError(err)
	}
	data := make([]dto.UserRoleResponse, 0, len(assignments))
	for _, a := range assignments {
		data = append(data, toUserRoleResponse(a))
	}
	return &dto.UserRolesResponse{UserID: userID.String(), Roles: data}, nil
}

// AssignRole grants a role to a user. Unknown role names are rejected here so
// they surface as a 400 rather than an opaque CHECK-constraint violation.
// Granting a role the user already holds is a no-op, not a conflict.
func (s *AdminService) AssignRole(ctx context.Context, userID uuid.UUID, roleName string, adminID uuid.UUID) error {
	role, ok := entity.ParseRole(roleName)
	if !ok {
		return errors.NewBadRequestError(fmt.Sprintf("unknown role %q", roleName))
	}
	if _, err := s.userStore.GetUserByID(ctx, userID); err != nil {
		return err
	}

	if err := s.roleStore.AssignRole(ctx, userID, role, &adminID); err != nil {
		logger.LogError(ctx, err, "admin: failed to assign role",
			"user_id", userID,
			"role", role,
		)
		return errors.NewInternalError(err)
	}

	logger.Info(ctx, "admin: role assigned",
		"user_id", userID,
		"role", role,
		"admin_id", adminID,
	)
	return nil
}

// RemoveRole revokes a role from a user. Revoking a role the user does not hold
// is a no-op.
func (s *AdminService) RemoveRole(ctx context.Context, userID uuid.UUID, roleName string, adminID uuid.UUID) error {
	role, ok := entity.ParseRole(roleName)
	if !ok {
		return errors.NewBadRequestError(fmt.Sprintf("unknown role %q", roleName))
	}

	if err := s.roleStore.RemoveRole(ctx, userID, role); err != nil {
		logger.LogError(ctx, err, "admin: failed to remove role",
			"user_id", userID,
			"role", role,
		)
		return errors.NewInternalError(err)
	}

	logger.Info(ctx, "admin: role removed",
		"user_id", userID,
		"role", role,
		"admin_id", adminID,
	)
	return nil
}

// ─── RBAC helper — implements middleware.RoleChecker ─────────────────────────

// UserHasAnyRole checks whether a user holds at least one of the named roles.
func (s *AdminService) UserHasAnyRole(ctx context.Context, userID uuid.UUID, roleNames []string) (bool, error) {
	return s.roleStore.UserHasAnyRole(ctx, userID, roleNames)
}

// ─── Stats ────────────────────────────────────────────────────────────────────

// GetStats returns basic platform statistics.
func (s *AdminService) GetStats(ctx context.Context) (*dto.AdminStatsResponse, error) {
	activeFlag := true
	inactiveFlag := false

	total, err := s.userStore.AdminCountUsers(ctx, AdminListUsersFilter{})
	if err != nil {
		logger.LogError(ctx, err, "admin: failed to count total users")
		return nil, errors.NewInternalError(err)
	}

	active, err := s.userStore.AdminCountUsers(ctx, AdminListUsersFilter{IsActive: &activeFlag})
	if err != nil {
		logger.LogError(ctx, err, "admin: failed to count active users")
		return nil, errors.NewInternalError(err)
	}

	inactive, err := s.userStore.AdminCountUsers(ctx, AdminListUsersFilter{IsActive: &inactiveFlag})
	if err != nil {
		logger.LogError(ctx, err, "admin: failed to count inactive users")
		return nil, errors.NewInternalError(err)
	}

	return &dto.AdminStatsResponse{
		TotalUsers:    total,
		ActiveUsers:   active,
		InactiveUsers: inactive,
		// Roles are a compile-time set, so this is a constant rather than a count.
		TotalRoles: int64(len(entity.AllRoles)),
	}, nil
}

// ─── Notification Management ──────────────────────────────────────────────────

// SendNotificationToUser sends a system announcement to a single user.
func (s *AdminService) SendNotificationToUser(ctx context.Context, adminID, targetUserID uuid.UUID, req *dto.AdminSendNotificationRequest) error {
	if s.notifEmitter == nil {
		return errors.New("NOTIFICATIONS_DISABLED", "notification service not configured", 503)
	}
	if req.Message == "" {
		return errors.NewBadRequestError("message is required")
	}
	if _, err := s.userStore.GetUserByID(ctx, targetUserID); err != nil {
		return err
	}
	groupKey := fmt.Sprintf("system:%s", uuid.New().String())
	if err := s.notifEmitter.EmitSystemAnnouncement(ctx, adminID, targetUserID, req.Message, groupKey); err != nil {
		logger.LogError(ctx, err, "admin: failed to send notification to user",
			"admin_id", adminID, "target_user_id", targetUserID)
		return errors.NewInternalError(err)
	}
	logger.Info(ctx, "admin: notification sent to user", "admin_id", adminID, "target_user_id", targetUserID)
	return nil
}

// BroadcastNotification sends a system announcement to all active users.
// Errors per-user are logged and skipped; the method returns the count of successful sends.
func (s *AdminService) BroadcastNotification(ctx context.Context, adminID uuid.UUID, req *dto.AdminSendNotificationRequest) (*dto.AdminBroadcastNotificationResponse, error) {
	if s.notifEmitter == nil {
		return nil, errors.New("NOTIFICATIONS_DISABLED", "notification service not configured", 503)
	}
	if req.Message == "" {
		return nil, errors.NewBadRequestError("message is required")
	}

	userIDs, err := s.userStore.ListAllActiveUserIDs(ctx)
	if err != nil {
		logger.LogError(ctx, err, "admin: failed to list active users for broadcast")
		return nil, errors.NewInternalError(err)
	}

	// One group_key shared across all recipients of this broadcast so it can be
	// identified as a single event; uniqueness per recipient comes from recipient_id.
	groupKey := fmt.Sprintf("system:%s", uuid.New().String())

	var sent int
	for _, userID := range userIDs {
		// Skip sending to the admin themselves.
		if userID == adminID {
			continue
		}
		if err := s.notifEmitter.EmitSystemAnnouncement(ctx, adminID, userID, req.Message, groupKey); err != nil {
			logger.LogError(ctx, err, "admin: broadcast notification failed for user", "user_id", userID)
			continue
		}
		sent++
	}

	logger.Info(ctx, "admin: broadcast notification sent", "admin_id", adminID, "sent", sent, "total", len(userIDs))
	return &dto.AdminBroadcastNotificationResponse{SentCount: sent}, nil
}

// ─── Private helpers ─────────────────────────────────────────────────────────

func toAdminUserResponse(u *entity.User, s storage.Storage) dto.AdminUserResponse {
	resp := dto.AdminUserResponse{
		ID:             u.ID.String(),
		Username:       u.Username,
		Email:          u.Email,
		DisplayName:    u.DisplayName,
		IsActive:       u.IsActive,
		FollowerCount:  u.FollowerCount,
		FollowingCount: u.FollowingCount,
		CreatedAt:      u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if u.AvatarKey != nil {
		url := s.URL(*u.AvatarKey)
		resp.AvatarURL = &url
	}
	if u.UpdatedAt != nil {
		t := u.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
		resp.UpdatedAt = &t
	}
	return resp
}

func toUserRoleResponse(a *entity.RoleAssignment) dto.UserRoleResponse {
	resp := dto.UserRoleResponse{
		Role:        a.Role.String(),
		Description: a.Role.Description(),
		AssignedAt:  a.AssignedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if a.AssignedBy != nil {
		id := a.AssignedBy.String()
		resp.AssignedBy = &id
	}
	return resp
}
