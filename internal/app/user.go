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

	// Services (account mail — welcome/verify/reset, plus delivery reports)
	accountMailService *service.AccountMailService
	emailEventService  *service.EmailEventService

	// Handlers
	userHandler    *handler.UserHandler
	authHandler    *handler.AuthHandler
	profileHandler *handler.ProfileHandler
	followHandler  *handler.FollowHandler
	emailHandler   *handler.EmailHandler
	// emailWebhookHandler is nil when no webhook secret is configured, which
	// leaves the route unregistered rather than open and unverified.
	emailWebhookHandler *handler.EmailWebhookHandler
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
func SetupUserContext(pool *pgxpool.Pool, jwtService *jwt.Service, store storage.Storage, refreshTokenExpiry time.Duration, secureCookie bool, mail *mailInfra) *UserContext {
	// Repositories
	userRepo := repository.NewUserRepository(pool)
	refreshTokenRepo := repository.NewRefreshTokenRepository(pool)
	followRepo := repository.NewFollowRepository(pool)
	emailTokenRepo := repository.NewEmailTokenRepository(pool)
	emailDeliveryRepo := repository.NewEmailDeliveryRepository(pool)

	// Services
	userService := service.NewUserService(userRepo, store)
	refreshTokenService := service.NewRefreshTokenServiceWithExpiry(refreshTokenRepo, refreshTokenExpiry)
	authService := service.NewAuthService(userRepo, userService, jwtService, refreshTokenService, store)
	followService := service.NewFollowService(followRepo)
	emailEventService := service.NewEmailEventService(emailDeliveryRepo)
	accountMailService := service.NewAccountMailService(mail.mailer, mail.templates, emailTokenRepo, userRepo, emailEventService, mail.baseURL)

	// Wire email sender into auth service for fire-and-forget after register
	authService.WithEmailSender(accountMailService)

	// Handlers
	userHandler := handler.NewUserHandler(userService, userService, store)
	authHandler := handler.NewAuthHandler(authService, store, secureCookie)
	profileHandler := handler.NewProfileHandler(userService, followService, store)
	followHandler := handler.NewFollowHandler(followService, userService)
	emailHandler := handler.NewEmailHandler(accountMailService)

	// Without a verifier there is nothing to authenticate the webhook with, so the
	// handler — and therefore the route — is left out entirely.
	var emailWebhookHandler *handler.EmailWebhookHandler
	if mail.verifier != nil {
		emailWebhookHandler = handler.NewEmailWebhookHandler(mail.verifier, emailEventService)
	}

	return &UserContext{
		userRepo:            userRepo,
		refreshTokenRepo:    refreshTokenRepo,
		followRepo:          followRepo,
		userService:         userService,
		refreshTokenService: refreshTokenService,
		authService:         authService,
		followService:       followService,
		accountMailService:  accountMailService,
		emailEventService:   emailEventService,
		userHandler:         userHandler,
		authHandler:         authHandler,
		profileHandler:      profileHandler,
		followHandler:       followHandler,
		emailHandler:        emailHandler,
		emailWebhookHandler: emailWebhookHandler,
	}
}

// SuppressionChecker exposes the suppression source for the mailer's gate.
func (ctx *UserContext) SuppressionChecker() mailer.SuppressionChecker {
	return ctx.emailEventService
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

func (ctx *UserContext) WireFeedEventEmitter(e service.FollowFeedEventEmitter) {
	ctx.followService.WithFeedEventEmitter(e)
}

func (ctx *UserContext) WireNotificationEmitter(notif *NotificationContext) {
	ctx.followService.WithNotificationEmitter(notif.notifService)
}
