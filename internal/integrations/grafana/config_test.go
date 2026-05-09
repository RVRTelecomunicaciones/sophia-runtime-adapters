package grafana_test

import (
	"strings"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/grafana"
)

func TestConfig_LoadFromEnv_HappyPath(t *testing.T) {
	t.Setenv("GRAFANA_URL", "http://grafana:3000")
	t.Setenv("GRAFANA_SERVICE_ACCOUNT_TOKEN", "glsa_xxxx")
	t.Setenv("LISTEN_ADDR", ":9096")
	t.Setenv("GRAFANA_TENANT_TYPE", "test")
	t.Setenv("RUNTIME_TENANT", "test")
	t.Setenv("CI", "")

	cfg, err := grafana.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.GrafanaURL != "http://grafana:3000" {
		t.Errorf("GrafanaURL: got %q", cfg.GrafanaURL)
	}
	if cfg.ServiceAccountToken != "glsa_xxxx" {
		t.Errorf("ServiceAccountToken: got %q", cfg.ServiceAccountToken)
	}
	if cfg.ListenAddr != ":9096" {
		t.Errorf("ListenAddr: got %q", cfg.ListenAddr)
	}
}

func TestConfig_RejectsInvalidGrafanaURL(t *testing.T) {
	t.Setenv("GRAFANA_URL", "not a url::")
	t.Setenv("GRAFANA_SERVICE_ACCOUNT_TOKEN", "glsa_xxxx")
	t.Setenv("CI", "")

	_, err := grafana.LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "URL") {
		t.Errorf("err: got %v, want URL parse error", err)
	}
}

func TestConfig_RejectsEmptyTokenUnderCI(t *testing.T) {
	t.Setenv("CI", "true")
	t.Setenv("GRAFANA_URL", "http://grafana:3000")
	t.Setenv("GRAFANA_SERVICE_ACCOUNT_TOKEN", "")
	t.Setenv("RUNTIME_TENANT", "test")
	t.Setenv("GRAFANA_TENANT_TYPE", "test")

	_, err := grafana.LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "TOKEN") {
		t.Errorf("err: got %v, want token-required error under CI", err)
	}
}

func TestConfig_TenantTypeRequiredUnderCI(t *testing.T) {
	t.Setenv("CI", "true")
	t.Setenv("GRAFANA_URL", "http://grafana:3000")
	t.Setenv("GRAFANA_SERVICE_ACCOUNT_TOKEN", "glsa_xxxx")
	t.Setenv("RUNTIME_TENANT", "test")
	t.Setenv("GRAFANA_TENANT_TYPE", "")

	_, err := grafana.LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "GRAFANA_TENANT_TYPE") {
		t.Errorf("err: got %v, want GRAFANA_TENANT_TYPE-required error under CI", err)
	}
}
