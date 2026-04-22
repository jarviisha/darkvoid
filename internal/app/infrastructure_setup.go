package app

import (
	"fmt"

	"github.com/jarviisha/darkvoid/internal/infrastructure/mailer"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

func (app *Application) setupInfrastructure() (storage.Storage, *mailer.Templates, mailer.Mailer, error) {
	store, err := storage.New(storage.Config{
		Provider: app.cfg.Storage.Provider,
		BaseURL:  app.cfg.Storage.BaseURL,
		LocalDir: app.cfg.Storage.LocalDir,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to initialize storage: %w", err)
	}
	app.log.Info("storage initialized", "provider", app.cfg.Storage.Provider, "base_url", app.cfg.Storage.BaseURL)

	m := mailer.New(mailer.Config{
		Provider: app.cfg.Mailer.Provider,
		Host:     app.cfg.Mailer.Host,
		Port:     app.cfg.Mailer.Port,
		Username: app.cfg.Mailer.Username,
		Password: app.cfg.Mailer.Password,
		From:     app.cfg.Mailer.From,
		BaseURL:  app.cfg.Mailer.BaseURL,
	})
	app.log.Info("mailer initialized", "provider", app.cfg.Mailer.Provider)

	templates, err := mailer.LoadTemplates()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load email templates: %w", err)
	}

	return store, templates, m, nil
}
