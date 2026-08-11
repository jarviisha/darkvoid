package config

import (
	"strings"
	"testing"
)

func TestLoadServerConfig_TrustedProxyCIDRs(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8, 2001:db8::/32")

	got := loadServerConfig().TrustedProxyCIDRs
	if len(got) != 2 || got[0] != "10.0.0.0/8" || got[1] != "2001:db8::/32" {
		t.Fatalf("TrustedProxyCIDRs = %v", got)
	}
}

func TestValidate_RejectsInvalidTrustedProxyCIDR(t *testing.T) {
	cfg := validCookieConfig()
	cfg.Server.TrustedProxyCIDRs = []string{"not-a-cidr"}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "trusted proxy CIDR") {
		t.Fatalf("Validate() error = %v, want trusted proxy CIDR error", err)
	}
}
