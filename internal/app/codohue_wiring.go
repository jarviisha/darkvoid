package app

import (
	"context"
	"time"

	"github.com/jarviisha/darkvoid/pkg/codohue"
)

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
