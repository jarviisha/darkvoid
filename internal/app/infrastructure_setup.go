package app

import (
	"context"
	"fmt"

	"github.com/jarviisha/darkvoid/internal/infrastructure/mailer"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

// mailInfra bundles the mail pieces built during infrastructure setup.
//
// The gate and the verifier are kept alongside the Mailer because both connect
// to the user context, which does not exist yet at this point: the gate needs the
// suppression source wired into it afterwards, and the verifier is handed to a
// handler the user context owns.
type mailInfra struct {
	// mailer is what everything sends through — the gate, as an interface.
	mailer    mailer.Mailer
	gate      *mailer.SuppressionGate
	templates *mailer.Templates
	// verifier is nil when no webhook secret is configured, which leaves the
	// webhook route unregistered.
	verifier *mailer.ResendWebhookVerifier
	baseURL  string
}

func (app *Application) setupInfrastructure(ctx context.Context) (storage.Storage, *mailInfra, error) {
	store, err := storage.NewWithContext(ctx, storage.Config{
		Provider: app.cfg.Storage.Provider,
		BaseURL:  app.cfg.Storage.BaseURL,
		LocalDir: app.cfg.Storage.LocalDir,
		S3: storage.S3Config{
			Endpoint:        app.cfg.Storage.S3.Endpoint,
			Region:          app.cfg.Storage.S3.Region,
			Bucket:          app.cfg.Storage.S3.Bucket,
			AccessKeyID:     app.cfg.Storage.S3.AccessKeyID,
			SecretAccessKey: app.cfg.Storage.S3.SecretAccessKey,
			SessionToken:    app.cfg.Storage.S3.SessionToken,
			UsePathStyle:    app.cfg.Storage.S3.UsePathStyle,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize storage: %w", err)
	}
	health, ok := store.(storage.HealthChecker)
	if !ok {
		return nil, nil, fmt.Errorf("storage provider %q does not implement health checks", app.cfg.Storage.Provider)
	}
	if err := health.HealthCheck(ctx); err != nil {
		return nil, nil, fmt.Errorf("storage health check failed: %w", err)
	}
	app.storageHealth = health
	app.log.Info("storage initialized", "provider", app.cfg.Storage.Provider, "base_url", app.cfg.Storage.BaseURL)

	m, err := mailer.New(mailer.Config{
		Provider: app.cfg.Mailer.Provider,
		Host:     app.cfg.Mailer.Host,
		Port:     app.cfg.Mailer.Port,
		Username: app.cfg.Mailer.Username,
		Password: app.cfg.Mailer.Password,
		APIKey:   app.cfg.Mailer.APIKey,
		From:     app.cfg.Mailer.From,
		BaseURL:  app.cfg.Mailer.BaseURL,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize mailer: %w", err)
	}

	// Every send goes through the gate so that suppression cannot be bypassed by a
	// call site that forgets to check.
	gate := mailer.NewSuppressionGate(m)

	templates, err := mailer.LoadTemplates()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load email templates: %w", err)
	}

	mail := &mailInfra{
		mailer:    gate,
		gate:      gate,
		templates: templates,
		baseURL:   app.cfg.Mailer.BaseURL,
	}

	if secret := app.cfg.Mailer.WebhookSecret; secret != "" {
		verifier, err := mailer.NewResendWebhookVerifier(secret)
		if err != nil {
			// Same reasoning as a missing API key: a secret that cannot be parsed
			// would reject every webhook, and silently serving without delivery
			// reports looks identical to a provider that never sends any.
			return nil, nil, fmt.Errorf("failed to initialize the mail webhook verifier: %w", err)
		}
		mail.verifier = verifier
	}

	app.log.Info("mailer initialized",
		"provider", app.cfg.Mailer.Provider,
		"webhook", mail.verifier != nil,
	)

	return store, mail, nil
}
