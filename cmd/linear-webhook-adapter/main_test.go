package main

import (
	"errors"
	"testing"
)

func TestLoadConfig_RequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"all required present", map[string]string{
			"LINEAR_API_TOKEN": "tok", "LINEAR_TEAM_ID": "team",
			"LINEAR_TENANT_TYPE": "test",
		}, false},
		{"missing api token", map[string]string{
			"LINEAR_TEAM_ID": "t", "LINEAR_TENANT_TYPE": "test",
		}, true},
		{"missing team id", map[string]string{
			"LINEAR_API_TOKEN": "tok", "LINEAR_TENANT_TYPE": "test",
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadConfig(mapEnv(tt.env))
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEnforceModeLock_AbortsInCIWithoutTestTenant(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"local + non-test tenant: ok", map[string]string{"CI": "", "RUNTIME_TENANT": "prod"}, false},
		{"CI + test tenant: ok", map[string]string{"CI": "true", "RUNTIME_TENANT": "test"}, false},
		{"CI + non-test tenant: ABORT", map[string]string{"CI": "true", "RUNTIME_TENANT": "prod"}, true},
		{"CI + missing RUNTIME_TENANT: ABORT", map[string]string{"CI": "true"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enforceModeLock(mapEnv(tt.env))
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEnforceTenantFingerprint_AbortsInCIWithProdTenantType(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"local + prod tenant_type: ok (operator override)", map[string]string{"CI": "", "LINEAR_TENANT_TYPE": "prod"}, false},
		{"CI + test tenant_type: ok", map[string]string{"CI": "true", "LINEAR_TENANT_TYPE": "test"}, false},
		{"CI + prod tenant_type: ABORT", map[string]string{"CI": "true", "LINEAR_TENANT_TYPE": "prod"}, true},
		{"CI + missing tenant_type: ABORT", map[string]string{"CI": "true"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enforceTenantFingerprint(mapEnv(tt.env))
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// mapEnv returns a closure that mimics os.Getenv but reads from a
// supplied map — lets tests run hermetically without mutating the
// process environment.
func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// Sanity check that the sentinel error is exposed for assertions.
func TestErrModeLockExposed(t *testing.T) {
	if errors.Is(errModeLock, nil) {
		t.Fatal("errModeLock must be a non-nil error")
	}
}
