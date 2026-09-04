package app

import (
	"testing"

	"github.com/jarviisha/darkvoid/pkg/config"
)

// TestSetupCodohueClient_EnabledWithoutBaseURLFailsBoot pins the misconfiguration
// that used to be invisible: CODOHUE_ENABLED=true with CODOHUE_BASE_URL left at
// its "" default. Config.Validate never inspects the Codohue block, and the
// client constructor answered with a nil *Client, which SetupFeedContext stored
// straight into the feed's Recommender field — a non-nil interface over a nil
// pointer. /health then reported "codohue: off" while the first feed request
// dereferenced the breaker. This is a config mistake, identical on every
// restart, so refusing to boot is the only outcome that names it.
func TestSetupCodohueClient_EnabledWithoutBaseURLFailsBoot(t *testing.T) {
	app := testApp(&config.Config{})
	app.cfg.Codohue.Enabled = true
	app.cfg.Codohue.BaseURL = ""

	client, err := app.setupCodohueClient()

	if err == nil {
		t.Fatal("setupCodohueClient() returned a nil error — want boot to fail when Codohue is enabled without a base URL")
	}
	if client != nil {
		t.Fatalf("setupCodohueClient() returned client %v — want nil alongside the error", client)
	}
}

// TestSetupCodohueClient_DisabledIsNotAFailure pins the other side of the same
// contract. CODOHUE_BASE_URL is empty on every deployment that never turned the
// integration on, so treating an empty URL as a boot failure unconditionally
// would refuse to start the majority of them. Disabled means nil client, no
// error — and nil is what every call site already reads as "integration off".
func TestSetupCodohueClient_DisabledIsNotAFailure(t *testing.T) {
	app := testApp(&config.Config{})
	app.cfg.Codohue.Enabled = false

	client, err := app.setupCodohueClient()

	if err != nil {
		t.Fatalf("setupCodohueClient() returned error %v — want none when Codohue is disabled", err)
	}
	if client != nil {
		t.Fatalf("setupCodohueClient() returned client %v — want nil so nothing wires the integration", client)
	}
}

// TestSetupCodohueClient_EnabledWithBaseURLBuildsAClient pins the happy path.
// The two cases above both expect a nil client, so on their own they are
// satisfied by a constructor that never builds anything — this is what keeps
// the "off" branch from widening to cover the "on" one.
func TestSetupCodohueClient_EnabledWithBaseURLBuildsAClient(t *testing.T) {
	app := testApp(&config.Config{})
	app.cfg.Codohue.Enabled = true
	app.cfg.Codohue.BaseURL = "http://codohue.invalid:2001"
	app.cfg.Codohue.Namespace = "darkvoid"

	client, err := app.setupCodohueClient()

	if err != nil {
		t.Fatalf("setupCodohueClient() returned error %v — want a client for an enabled, configured integration", err)
	}
	if client == nil {
		t.Fatal("setupCodohueClient() returned a nil client — want one built from the configured base URL")
	}
}
