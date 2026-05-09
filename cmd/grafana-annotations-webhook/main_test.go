package main

import (
	"errors"
	"testing"
)

func TestEnforceModeLock(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"local + non-test tenant: ok", map[string]string{"CI": "", "RUNTIME_TENANT": "prod"}, false},
		{"CI + test tenant: ok", map[string]string{"CI": "true", "RUNTIME_TENANT": "test"}, false},
		{"CI + non-test tenant: ABORT", map[string]string{"CI": "true", "RUNTIME_TENANT": "prod"}, true},
		{"CI + missing tenant: ABORT", map[string]string{"CI": "true"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enforceModeLock(func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.Is(err, errModeLock) {
				t.Errorf("err must wrap errModeLock, got %v", err)
			}
		})
	}
}

func TestEnforceTenantFingerprint(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"local: ok", map[string]string{"CI": "", "GRAFANA_TENANT_TYPE": "production"}, false},
		{"CI + test: ok", map[string]string{"CI": "true", "GRAFANA_TENANT_TYPE": "test"}, false},
		{"CI + production: ABORT", map[string]string{"CI": "true", "GRAFANA_TENANT_TYPE": "production"}, true},
		{"CI + missing: ABORT", map[string]string{"CI": "true"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enforceTenantFingerprint(func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.Is(err, errTenantFingerprint) {
				t.Errorf("err must wrap errTenantFingerprint, got %v", err)
			}
		})
	}
}
