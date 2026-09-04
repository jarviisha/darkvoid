package app

import (
	"fmt"

	"github.com/jarviisha/darkvoid/pkg/codohue"
)

// setupCodohueClient builds the Codohue client, or reports why it cannot.
//
// It is built here rather than inside SetupFeedContext because the post services
// need the same client for catalog ingest and object deletion, and they are
// constructed before the feed context is. One client, not two: the circuit
// breaker's state is per-client, so a second one would learn about an outage
// separately and report a different answer to /health than the feed experiences.
//
// It must run after ensureCodohueNamespaceConfig, which is what fills in
// cfg.Codohue.NamespaceKey.
func (app *Application) setupCodohueClient() (*codohue.Client, error) {
	if !app.cfg.Codohue.Enabled {
		// A nil client is not a missing value here, it is the documented
		// "integration off" one, and every call site already branches on it
		// rather than on a sentinel error.
		return nil, nil //nolint:nilnil // nil client is the documented "off" value
	}

	client, err := codohue.NewClient(
		app.cfg.Codohue.BaseURL,
		app.cfg.Codohue.NamespaceKey,
		app.cfg.Codohue.Namespace,
		app.codohueEventsClient(),
	)
	if err != nil {
		return nil, fmt.Errorf("codohue is enabled but its client cannot be built: %w", err)
	}
	return client, nil
}
