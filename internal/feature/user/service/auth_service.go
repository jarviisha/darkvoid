package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

const postRegisterEmailTimeout = 30 * time.Second

// emailSender sends fire-and-forget emails after registration.
type emailSender interface {
	SendWelcome(ctx context.Context, email, username string)
	SendVerification(ctx context.Context, userID uuid.UUID, email, username string)
}

type userCreator interface {
	CreateUser(ctx context.Context, req *dto.CreateUserRequest) (uuid.UUID, error)
}

type refreshTokenManager interface {
	GenerateToken(ctx context.Context, userID uuid.UUID) (*entity.RefreshToken, error)
	ValidateToken(ctx context.Context, token string) (uuid.UUID, error)
	RevokeToken(ctx context.Context, token string) error
	RevokeAllUserTokens(ctx context.Context, userID uuid.UUID) error
	GetExpiryDuration() time.Duration
}

type accessTokenManager interface {
	GenerateToken(subject string) (string, error)
	GetExpiryDuration() time.Duration
}

// AuthService handles authentication business logic.
type AuthService struct {
	userRepo            userRepo
	userService         userCreator
	accessTokenService  accessTokenManager
	refreshTokenService refreshTokenManager
	storage             storage.Storage
	emailSender         emailSender // optional, set via WithEmailSender
}

func NewAuthService(userRepo userRepo, userCreator userCreator, accessTokenService accessTokenManager, refreshTokenService refreshTokenManager, store storage.Storage) *AuthService {
	return &AuthService{
		userRepo:            userRepo,
		userService:         userCreator,
		accessTokenService:  accessTokenService,
		refreshTokenService: refreshTokenService,
		storage:             store,
	}
}

// WithEmailSender sets the optional email sender for post-registration emails.
func (s *AuthService) WithEmailSender(sender emailSender) {
	s.emailSender = sender
}

func (s *AuthService) Register(ctx context.Context, req *dto.RegisterRequest) (*dto.RegisterResponse, error) {
	logger.Info(ctx, "register attempt", "username", req.Username, "email", req.Email)

	userID, err := s.userService.CreateUser(ctx, &dto.CreateUserRequest{
		Username:    req.Username,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Password:    req.Password,
	})
	if err != nil {
		return nil, err
	}

	accessToken, err := s.accessTokenService.GenerateToken(userID.String())
	if err != nil {
		logger.LogError(ctx, err, "failed to generate access token after register")
		return nil, errors.NewInternalError(err)
	}

	refreshToken, err := s.refreshTokenService.GenerateToken(ctx, userID)
	if err != nil {
		logger.LogError(ctx, err, "failed to generate refresh token after register")
		return nil, errors.NewInternalError(err)
	}

	// Fire-and-forget: send welcome + verification emails
	if s.emailSender != nil {
		go func() {
			emailCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postRegisterEmailTimeout)
			defer cancel()
			s.emailSender.SendWelcome(emailCtx, req.Email, req.Username)
		}()
		go func() {
			emailCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postRegisterEmailTimeout)
			defer cancel()
			s.emailSender.SendVerification(emailCtx, userID, req.Email, req.Username)
		}()
	}

	logger.Info(ctx, "user registered successfully", "user_id", userID)
	return &dto.RegisterResponse{
		UserID:           userID.String(),
		AccessToken:      accessToken,
		RefreshToken:     refreshToken.Token,
		TokenType:        "Bearer",
		AccessExpiresIn:  int64(s.accessTokenService.GetExpiryDuration().Seconds()),
		RefreshExpiresIn: int64(s.refreshTokenService.GetExpiryDuration().Seconds()),
	}, nil
}

func (s *AuthService) Login(ctx context.Context, req *dto.LoginRequest) (*dto.LoginResponse, error) {
	logger.Info(ctx, "user login attempt", "username", req.Username)

	if strings.TrimSpace(req.Username) == "" {
		return nil, errors.NewBadRequestError("username is required")
	}
	if req.Password == "" {
		return nil, errors.NewBadRequestError("password is required")
	}

	u, err := s.userRepo.GetUserByUsername(ctx, strings.TrimSpace(req.Username))
	if err != nil {
		logger.Warn(ctx, "user not found or error", "username", req.Username, "error", err)
		return nil, errors.NewUnauthorizedError("invalid username or password")
	}

	verify, err := verifyPassword(u.PasswordHash, req.Password)
	if err != nil || !verify {
		logger.Warn(ctx, "invalid password", "username", req.Username)
		return nil, errors.NewUnauthorizedError("invalid username or password")
	}

	if !u.IsActive {
		logger.Warn(ctx, "inactive user login attempt", "username", req.Username)
		return nil, errors.NewForbiddenError("user account is deactivated")
	}

	accessToken, err := s.accessTokenService.GenerateToken(u.ID.String())
	if err != nil {
		logger.LogError(ctx, err, "failed to generate access token")
		return nil, errors.NewInternalError(err)
	}

	refreshToken, err := s.refreshTokenService.GenerateToken(ctx, u.ID)
	if err != nil {
		logger.LogError(ctx, err, "failed to generate refresh token")
		return nil, errors.NewInternalError(err)
	}

	logger.Info(ctx, "user logged in successfully", "user_id", u.ID)
	return &dto.LoginResponse{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken.Token,
		AccessExpiresIn:  int64(s.accessTokenService.GetExpiryDuration().Seconds()),
		RefreshExpiresIn: int64(s.refreshTokenService.GetExpiryDuration().Seconds()),
		TokenType:        "Bearer",
		User:             dto.ToUserResponse(u, s.storage),
	}, nil
}

func (s *AuthService) RefreshAccessToken(ctx context.Context, req *dto.RefreshTokenRequest) (*dto.RefreshTokenResponse, error) {
	if strings.TrimSpace(req.RefreshToken) == "" {
		return nil, errors.NewBadRequestError("refresh token is required")
	}

	userID, err := s.refreshTokenService.ValidateToken(ctx, req.RefreshToken)
	if err != nil {
		logger.Warn(ctx, "invalid refresh token", "error", err)
		return nil, err
	}

	u, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, errors.NewUnauthorizedError("invalid refresh token")
	}

	if !u.IsActive {
		return nil, errors.NewForbiddenError("user account is deactivated")
	}

	accessToken, err := s.accessTokenService.GenerateToken(u.ID.String())
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	if err = s.refreshTokenService.RevokeToken(ctx, req.RefreshToken); err != nil {
		logger.LogError(ctx, err, "failed to revoke old refresh token")
	}

	newRefreshToken, err := s.refreshTokenService.GenerateToken(ctx, u.ID)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	logger.Info(ctx, "access token refreshed successfully", "user_id", u.ID)
	return &dto.RefreshTokenResponse{
		AccessToken:      accessToken,
		RefreshToken:     newRefreshToken.Token,
		AccessExpiresIn:  int64(s.accessTokenService.GetExpiryDuration().Seconds()),
		RefreshExpiresIn: int64(s.refreshTokenService.GetExpiryDuration().Seconds()),
		TokenType:        "Bearer",
	}, nil
}

func (s *AuthService) Logout(ctx context.Context, req *dto.LogoutRequest) error {
	if strings.TrimSpace(req.RefreshToken) == "" {
		return errors.NewBadRequestError("refresh token is required")
	}

	if err := s.refreshTokenService.RevokeToken(ctx, req.RefreshToken); err != nil {
		logger.LogError(ctx, err, "failed to revoke refresh token during logout")
		return err
	}

	logger.Info(ctx, "user logged out successfully")
	return nil
}

func (s *AuthService) LogoutAllSessions(ctx context.Context, userID uuid.UUID) error {
	if err := s.refreshTokenService.RevokeAllUserTokens(ctx, userID); err != nil {
		logger.LogError(ctx, err, "failed to revoke all user tokens", "user_id", userID)
		return err
	}

	logger.Info(ctx, "all user sessions logged out successfully", "user_id", userID)
	return nil
}

func (s *AuthService) GetMe(ctx context.Context, userID uuid.UUID) (*entity.User, error) {
	u, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, errors.NewNotFoundError("user not found")
	}

	if !u.IsActive {
		return nil, errors.NewForbiddenError("user account is deactivated")
	}

	return u, nil
}

func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, oldPassword, newPassword string) error {
	if oldPassword == "" {
		return errors.NewBadRequestError("old password is required")
	}
	if newPassword == "" {
		return errors.NewBadRequestError("new password is required")
	}

	u, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return errors.NewNotFoundError("user not found")
	}

	if !u.IsActive {
		return errors.NewForbiddenError("user account is deactivated")
	}

	verify, err := verifyPassword(u.PasswordHash, oldPassword)
	if err != nil || !verify {
		return errors.NewUnauthorizedError("invalid old password")
	}

	hashedPassword, err := hashPassword(newPassword)
	if err != nil {
		return errors.NewInternalError(err)
	}

	if err := s.userRepo.UpdateUserPassword(ctx, userID, hashedPassword, nil); err != nil {
		return errors.NewInternalError(err)
	}

	if err := s.refreshTokenService.RevokeAllUserTokens(ctx, userID); err != nil {
		logger.LogError(ctx, err, "failed to revoke refresh tokens after password change")
	}

	logger.Info(ctx, "password changed successfully", "user_id", userID)
	return nil
}
