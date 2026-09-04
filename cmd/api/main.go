package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jarviisha/darkvoid/internal/app"
	"github.com/jarviisha/darkvoid/pkg/config"
	"github.com/jarviisha/darkvoid/pkg/logger"

	_ "github.com/jarviisha/darkvoid/docs" // Import generated Swagger docs
)

//	@title			DarkVoid API
//	@version		1.0
//	@description	Social network platform API using bounded context architecture
//	@termsOfService	http://swagger.io/terms/

//	@contact.name	API Support
//	@contact.email	support@darkvoid.com

//	@license.name	MIT
//	@license.url	https://opensource.org/licenses/MIT

//	@host		localhost:8080
//	@BasePath	/api/v1

// @securityDefinitions.apikey	BearerAuth
// @in							header
// @name						Authorization
// @description				Type "Bearer" followed by a space and JWT token.
func main() {
	ctx := context.Background()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fatal(ctx, "failed to load config", err)
	}

	// Initialize application
	application, err := app.New(ctx, cfg)
	if err != nil {
		fatal(ctx, "failed to initialize application", err)
	}

	// Start application server in goroutine
	go func() {
		if err := application.Start(); err != nil {
			fatal(ctx, "failed to start application", err)
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	// Graceful shutdown with 30 second timeout
	if err := application.GracefulShutdown(30 * time.Second); err != nil {
		fatal(ctx, "failed to gracefully shutdown", err)
	}
}

// fatal reports an unrecoverable startup or shutdown failure and stops the
// process.
//
// Not log.Fatalf: app.New installs this logger as the slog default, and
// slog.SetDefault also redirects the standard log package through that handler
// at Info level. Every one of these four failures was therefore reported at
// INFO — the least severe level the process emits, carrying its most severe
// meaning — so an alert filtering on ERROR saw nothing when the API refused to
// start. The exit code was always 1 and stays 1.
func fatal(ctx context.Context, msg string, err error) {
	logger.LogError(ctx, err, msg)
	os.Exit(1)
}
