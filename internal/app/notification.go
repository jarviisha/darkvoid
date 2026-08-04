package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jarviisha/darkvoid/internal/feature/notification/broker"
	notifcache "github.com/jarviisha/darkvoid/internal/feature/notification/cache"
	notifentity "github.com/jarviisha/darkvoid/internal/feature/notification/entity"
	notifhandler "github.com/jarviisha/darkvoid/internal/feature/notification/handler"
	"github.com/jarviisha/darkvoid/internal/feature/notification/repository"
	notifservice "github.com/jarviisha/darkvoid/internal/feature/notification/service"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

// NotificationContext represents the Notification bounded context.
type NotificationContext struct {
	notifService *notifservice.NotificationService
	notifHandler *notifhandler.NotificationHandler
	broker       *broker.Broker // nil when Redis is disabled
}

type NotificationPorts struct {
	Service *notifservice.NotificationService
	Broker  *broker.Broker
}

type notificationUserReader interface {
	GetAuthorsByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*notifentity.Actor, error)
}

func (ctx *NotificationContext) Ports() NotificationPorts {
	return NotificationPorts{
		Service: ctx.notifService,
		Broker:  ctx.broker,
	}
}

// SetupNotificationContext initializes the Notification context with all required dependencies.
// redisClient is required and must be non-nil: it backs the unread-count cache and
// the Pub/Sub channel that fans SSE events out across instances.
// Wiring NotifService into producing services (Post, Like, Comment, Follow) is the caller's
// responsibility and must be done in app.go after all contexts are initialized.
func SetupNotificationContext(pool *pgxpool.Pool, store storage.Storage, userReader notificationUserReader, redisClient *pkgredis.Client) *NotificationContext {
	notifRepo := repository.NewNotificationRepository(pool)

	nc := notifcache.NewRedisNotificationCache(redisClient)

	// Build SSE broker
	b := broker.NewBroker(redisClient)

	notifSvc := notifservice.NewNotificationService(notifRepo, nc, userReader)
	notifSvc.WithBroker(b)

	notifHdlr := notifhandler.NewNotificationHandler(notifSvc, store)
	notifHdlr.WithBroker(b)

	return &NotificationContext{
		notifService: notifSvc,
		notifHandler: notifHdlr,
		broker:       b,
	}
}

// StartBroker starts the Redis Pub/Sub subscriber in a background goroutine.
// Should be called after the application is fully initialized.
// No-op when Redis is disabled (broker operates in-memory only).
func (ctx *NotificationContext) StartBroker(appCtx context.Context) {
	if ctx.broker != nil {
		go ctx.broker.StartRedisSubscriber(appCtx)
	}
}
