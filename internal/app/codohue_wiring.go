package app

import (
	"context"
	"fmt"
	"time"

	"github.com/jarviisha/darkvoid/pkg/codohue"
)

func (app *Application) ensureCodohueNamespaceConfig(ctx context.Context) error {
	if !app.cfg.Codohue.Enabled {
		return nil
	}

	provisionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result, err := codohue.ProvisionNamespaceConfig(provisionCtx, codohue.NamespaceProvisionConfig{
		BaseURL:      app.cfg.Codohue.BaseURL,
		AdminKey:     app.cfg.Codohue.AdminKey,
		Namespace:    app.cfg.Codohue.Namespace,
		EmbeddingDim: app.cfg.Codohue.EmbeddingDim,
	})
	if err != nil {
		return fmt.Errorf("provision codohue namespace config: %w", err)
	}

	if result.APIKey != "" {
		app.cfg.Codohue.NamespaceKey = result.APIKey
	}
	if app.cfg.Codohue.NamespaceKey == "" {
		return fmt.Errorf("codohue namespace %q already exists but CODOHUE_NAMESPACE_KEY is not configured", app.cfg.Codohue.Namespace)
	}

	app.log.Info("codohue namespace config sent",
		"namespace", result.Namespace,
		"embedding_dim", app.cfg.Codohue.EmbeddingDim,
	)
	return nil
}

func (app *Application) wireCodohue(ctx context.Context, codohueClient *codohue.Client) {
	if codohueClient == nil {
		app.log.Info("codohue disabled, skipping post service wiring")
		return
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := codohueClient.Ping(pingCtx); err != nil {
		app.log.Warn("codohue service unreachable, skipping post side-effect wiring",
			"base_url", app.cfg.Codohue.BaseURL,
			"error", err,
		)
		return
	}

	app.Post.WireCodohue(codohueClient, app.cfg.Codohue.EmbeddingDim)
	app.log.Info("codohue client wired into post services",
		"namespace", app.cfg.Codohue.Namespace,
		"embedding_dim", app.cfg.Codohue.EmbeddingDim,
	)
	app.log.Info("codohue service reachable", "base_url", app.cfg.Codohue.BaseURL)
}
