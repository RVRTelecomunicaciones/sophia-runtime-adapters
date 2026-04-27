package chaos

import "errors"

// ChaosConfigEnv is the env-driven chaos configuration source. Populated
// by internal/infrastructure/config from RUNTIME_CHAOS_ENABLED and
// RUNTIME_CHAOS_PROFILE env vars.
type ChaosConfigEnv struct {
	Enabled     bool
	ProfilePath string
}

// LoadConfig resolves the chaos config given the env-driven inputs.
// R17 fail-closed semantics:
//   - Returns &Config{Enabled: false, Env: runtimeEnv} when env.Enabled is false.
//   - Returns error if runtimeEnv == "production" AND env.Enabled is true.
//   - Returns error if env.Enabled is true AND env.ProfilePath is empty.
//   - Returns error from LoadProfile (allowlist, schema, etc.) if any check fails.
//   - On success: &Config{Enabled: true, Profile: loaded, Env: runtimeEnv}.
//
// runtimeEnv must be one of "development" | "staging" | "production".
// Other values are passed through to Config.Env (LoadConfig itself does
// not validate the enum; the config package does that at parse time).
func LoadConfig(env ChaosConfigEnv, runtimeEnv string, cat Catalog) (*Config, error) {
	if !env.Enabled {
		return &Config{Enabled: false, Env: runtimeEnv}, nil
	}
	if runtimeEnv == "production" {
		return nil, errors.New("chaos: refuses to start with RUNTIME_ENV=production")
	}
	if env.ProfilePath == "" {
		return nil, errors.New("chaos: RUNTIME_CHAOS_PROFILE required when enabled")
	}
	profile, err := LoadProfile(env.ProfilePath, cat)
	if err != nil {
		return nil, err // LoadProfile already prefixes "chaos:"
	}
	return &Config{Enabled: true, Profile: profile, Env: runtimeEnv}, nil
}
