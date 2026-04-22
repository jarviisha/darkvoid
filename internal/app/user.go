package app

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/internal/feature/user/handler"
	"github.com/jarviisha/darkvoid/internal/feature/user/repository"
	"github.com/jarviisha/darkvoid/internal/feature/user/service"
	"github.com/jarviisha/darkvoid/internal/infrastructure/mailer"
	"github.com/jarviisha/darkvoid/pkg/jwt"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

// UserContext represents the User bounded context with all its dependencies.
type UserContext struct {
	// Repositories
	userRepo         *repository.UserRepository
	refreshTokenRepo *repository.RefreshTokenRepository
	followRepo       *repository.FollowRepository

	// Services
	userService         *service.UserService
	refreshTokenService *service.RefreshTokenService
	authService         *service.AuthService
	followService       *service.FollowService

	// Services (account mail — welcome/verify/reset)
	accountMailService *service.AccountMailService

	// Handlers
	userHandler    *handler.UserHandler
	authHandler    *handler.AuthHandler
	profileHandler *handler.ProfileHandler
	followHandler  *handler.FollowHandler
	emailHandler   *handler.EmailHandler
}

type UserPorts struct {
	FeedUserRepo         feedUserRepo
	FeedFollowService    feedFollowService
	PostUserRepo         postUserRepo
	PostFollowService    postFollowService
	NotificationUserRepo notificationUserRepo
	SearchUserRepo       userSearchRepo
	AdminUserStore       adminUserStoreSource
}

// SetupUserContext initializes the User context with all required dependencies.
// secureCookie controls the Secure flag on the refresh token cookie — set to false in development (HTTP).
func SetupUserContext(pool *pgxpool.Pool, jwtService *jwt.Service, store storage.Storage, refreshTokenExpiry time.Duration, secureCookie bool, m mailer.Mailer, templates *mailer.Templates, mailerBaseURL string) *UserContext {
	// Repositories
	userRepo := repository.NewUserRepository(pool)
	refreshTokenRepo := repository.NewRefreshTokenRepository(pool)
	followRepo := repository.NewFollowRepository(pool)
	emailTokenRepo := repository.NewEmailTokenRepository(pool)

	// Services
	userService := service.NewUserService(userRepo, store)
	refreshTokenService := service.NewRefreshTokenServiceWithExpiry(refreshTokenRepo, refreshTokenExpiry)
	authService := service.NewAuthService(userRepo, userService, jwtService, refreshTokenService, store)
	followService := service.NewFollowService(followRepo)
	accountMailService := service.NewAccountMailService(m, templates, emailTokenRepo, userRepo, mailerBaseURL)

	// Wire email sender into auth service for fire-and-forget after register
	authService.WithEmailSender(accountMailService)

	// Handlers
	userHandler := handler.NewUserHandler(userService, userService, store)
	authHandler := handler.NewAuthHandler(authService, store, secureCookie)
	profileHandler := handler.NewProfileHandler(userService, followService, store)
	followHandler := handler.NewFollowHandler(followService, userService)
	emailHandler := handler.NewEmailHandler(accountMailService)

	return &UserContext{
		userRepo:            userRepo,
		refreshTokenRepo:    refreshTokenRepo,
		followRepo:          followRepo,
		userService:         userService,
		refreshTokenService: refreshTokenService,
		authService:         authService,
		followService:       followService,
		accountMailService:  accountMailService,
		userHandler:         userHandler,
		authHandler:         authHandler,
		profileHandler:      profileHandler,
		followHandler:       followHandler,
		emailHandler:        emailHandler,
	}
}

func (ctx *UserContext) Ports() UserPorts {
	return UserPorts{
		FeedUserRepo:         ctx.userRepo,
		FeedFollowService:    ctx.followService,
		PostUserRepo:         buildPostUserRepo(ctx.userRepo),
		PostFollowService:    buildPostFollowService(ctx.followService),
		NotificationUserRepo: ctx.userRepo,
		SearchUserRepo:       ctx.userRepo,
		AdminUserStore:       ctx.userRepo,
	}
}

func (ctx *UserContext) WireFeedInvalidator(inv service.FeedInvalidator) {
	ctx.followService.WithFeedInvalidator(inv)
}

func (ctx *UserContext) WireNotificationEmitter(notif *NotificationContext) {
	ctx.followService.WithNotificationEmitter(notif.notifService)
}
