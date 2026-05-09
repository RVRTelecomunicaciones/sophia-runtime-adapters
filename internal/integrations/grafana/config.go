package grafana

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Config carries the runtime configuration of the
// grafana-annotations-webhook adapter. All fields are loaded from env
// vars at startup; mode-lock + tenant-fingerprint enforcement happens
// in cmd/grafana-annotations-webhook/main.go (Layers 3 + 4).
type Config struct {
	GrafanaURL          string // base URL of the Grafana instance, e.g. http://grafana:3000
	ServiceAccountToken string // Grafana service account token (Annotations:Write scope)
	ListenAddr          string // HTTP listen address, e.g. :9096
	TenantType          string // "test" | "production" — Layer 4 fingerprint (D2C4AB.16 extended to D)
	RuntimeTenant       string // mirror of RUNTIME_TENANT for Layer 3 mode lock
}

// LoadConfig reads env vars and validates them. Under CI=true, both
// GRAFANA_SERVICE_ACCOUNT_TOKEN and GRAFANA_TENANT_TYPE MUST be
// non-empty (Layer 2 + Layer 4 of the 4-layer guardrail).
func LoadConfig() (Config, error) {
	cfg := Config{
		GrafanaURL:          os.Getenv("GRAFANA_URL"),
		ServiceAccountToken: os.Getenv("GRAFANA_SERVICE_ACCOUNT_TOKEN"),
		ListenAddr:          envOr("LISTEN_ADDR", ":9096"),
		TenantType:          os.Getenv("GRAFANA_TENANT_TYPE"),
		RuntimeTenant:       os.Getenv("RUNTIME_TENANT"),
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate enforces presence + shape constraints on the loaded config.
func (c Config) Validate() error {
	if c.GrafanaURL == "" {
		return fmt.Errorf("GRAFANA_URL is required")
	}
	if !strings.HasPrefix(c.GrafanaURL, "http://") && !strings.HasPrefix(c.GrafanaURL, "https://") {
		return fmt.Errorf("GRAFANA_URL must start with http:// or https://, got %q", c.GrafanaURL)
	}
	if _, err := url.Parse(c.GrafanaURL); err != nil {
		return fmt.Errorf("GRAFANA_URL is not a valid URL: %w", err)
	}
	if isCI() {
		if c.ServiceAccountToken == "" {
			return fmt.Errorf("GRAFANA_SERVICE_ACCOUNT_TOKEN is required under CI=true")
		}
		if c.TenantType == "" {
			return fmt.Errorf("GRAFANA_TENANT_TYPE is required under CI=true (Layer 4 fingerprint)")
		}
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func isCI() bool {
	return strings.ToLower(os.Getenv("CI")) == "true"
}
