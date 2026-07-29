package app

import (
	"context"
	"fmt"
	"time"

	"github.com/jarviisha/darkvoid/pkg/codohue"
)

// codohueProbeInterval is how often the background monitor re-probes Codohue
// while it is degraded. Long enough that a sustained outage does not fill the
// log, short enough that recovery is noticed within a few minutes.
const codohueProbeInterval = 2 * time.Minute

// ensureCodohueNamespaceConfig provisions the Codohue namespace and resolves
// the namespace key. Errors are non-fatal by design: the caller downgrades
// them to a degraded state rather than taking the API down.
func (app *Application) ensureCodohueNamespaceConfig(ctx context.Context) error {
	if !app.cfg.Codohue.Enabled {
		return nil
	}

	provisionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result, err := codohue.ProvisionNamespaceConfig(provisionCtx, codohue.NamespaceProvisionConfig{
		AdminBaseURL: app.cfg.Codohue.AdminURL,
		AdminKey:     app.cfg.Codohue.AdminKey,
		Namespace:    app.cfg.Codohue.Namespace,
		EmbeddingDim: app.cfg.Codohue.EmbeddingDim,
		DenseSource:  app.cfg.Codohue.DenseSource,
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

// wireCodohue attaches the Codohue client to the post services.
//
// The client is wired whether or not Codohue answers right now. An unreachable
// Codohue is already handled per call — the feed logs and falls back to local
// scoring, and the ingest path logs and moves on — so gating the wiring on a
// startup probe bought nothing and cost everything: a Codohue that was down for
// the few seconds the API booted stayed off for the entire process, announced by
// a single log line. That is not hypothetical; it happened twice on the dev box
// when a hand-typed compose command recreated the API detached from Codohue's
// network, and both times it went unnoticed for the better part of an hour.
//
// The probe now only classifies: reachable means active, unreachable means
// degraded and the monitor keeps watching. Nothing is rewired later, so no
// service field is written once the server is serving.
func (app *Application) wireCodohue(ctx context.Context, codohueClient *codohue.Client) {
	if codohueClient == nil {
		app.codohue.set(CodohueOff, "")
		app.log.Info("codohue disabled, skipping post service wiring")
		return
	}

	app.Post.WireCodohue(codohueClient, app.cfg.Codohue.EmbeddingDim, app.cfg.Codohue.DenseSource)
	app.log.Info("codohue client wired into post services",
		"namespace", app.cfg.Codohue.Namespace,
		"embedding_dim", app.cfg.Codohue.EmbeddingDim,
		"dense_source", app.cfg.Codohue.DenseSource,
	)

	if err := app.probeCodohue(ctx, codohueClient); err != nil {
		app.log.Error("codohue unreachable at startup, serving degraded",
			"base_url", app.cfg.Codohue.BaseURL,
			"error", err,
			"effect", "feed falls back to local scoring; new posts are not indexed until it recovers",
		)
		app.startCodohueMonitor(codohueClient)
		return
	}

	app.log.Info("codohue service reachable", "base_url", app.cfg.Codohue.BaseURL)
}

// probeCodohue pings Codohue and records the outcome as the reported state.
func (app *Application) probeCodohue(ctx context.Context, client *codohue.Client) error {
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx); err != nil {
		app.codohue.set(CodohueDegraded, err.Error())
		return err
	}
	app.codohue.set(CodohueActive, "")
	return nil
}

// startCodohueMonitor re-probes a degraded Codohue until it answers, then stops.
//
// It exists so a degraded state is not a single line in the boot log: while it
// lasts, each probe re-states it at ERROR, and recovery is announced explicitly.
// Recovery needs no rewiring — the client was wired all along, so its calls start
// succeeding on their own.
func (app *Application) startCodohueMonitor(client *codohue.Client) {
	go func() {
		ticker := time.NewTicker(codohueProbeInterval)
		defer ticker.Stop()

		for {
			select {
			case <-app.runCtx.Done():
				return
			case <-ticker.C:
				if err := app.probeCodohue(app.runCtx, client); err != nil {
					app.log.Error("codohue still unreachable",
						"base_url", app.cfg.Codohue.BaseURL,
						"error", err,
					)
					continue
				}
				app.log.Info("codohue recovered, indexing and recommendations resume",
					"base_url", app.cfg.Codohue.BaseURL,
				)
				return
			}
		}
	}()
}
