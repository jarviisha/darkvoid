package config

import (
	"strings"
	"testing"
)

// The baseline leaves Codohue disabled, which is the common deployment. None of
// the checks below may fire in that state, or every install without the
// integration would stop booting.
func TestValidate_CodohueDisabledNeedsNoCredentials(t *testing.T) {
	cfg := validCookieConfig()
	cfg.Codohue = CodohueConfig{Enabled: false}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with Codohue off = %v, want nil", err)
	}
}

// The namespace key used to be optional because provisioning minted one and
// handed it back. It is now ours to send, so an empty value is a configuration
// error — and it has to surface here rather than three layers into provisioning,
// which is where it used to be noticed.
func TestValidate_CodohueEnabledRequiresNamespaceKey(t *testing.T) {
	cfg := validCookieConfig()
	cfg.Codohue = CodohueConfig{Enabled: true, BaseURL: "http://codohue:2001", Namespace: "darkvoid_feed"}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "CODOHUE_NAMESPACE_KEY") {
		t.Fatalf("Validate() error = %v, want a CODOHUE_NAMESPACE_KEY error naming the variable", err)
	}
}

// The admin token is only needed by the deployment that provisions, so it is
// gated on the admin URL rather than on Enabled. A runtime-only deployment
// pointed at an already-provisioned namespace must still boot.
func TestValidate_CodohueAdminTokenOnlyRequiredWithAdminURL(t *testing.T) {
	cfg := validCookieConfig()
	cfg.Codohue = CodohueConfig{
		Enabled:      true,
		BaseURL:      "http://codohue:2001",
		NamespaceKey: "namespace-key",
		Namespace:    "darkvoid_feed",
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() without an admin URL = %v, want nil", err)
	}

	cfg.Codohue.AdminURL = "http://codohue:2002"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "CODOHUE_ADMIN_TOKEN") {
		t.Fatalf("Validate() error = %v, want a CODOHUE_ADMIN_TOKEN error naming the variable", err)
	}

	cfg.Codohue.AdminToken = "admin-service-token"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with both credentials = %v, want nil", err)
	}
}
