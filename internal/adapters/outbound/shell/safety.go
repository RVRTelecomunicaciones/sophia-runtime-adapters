package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveCommand returns the absolute path to command per Config.
// Absolute paths are returned as-is (still existence-checked).
// Relative-name commands are resolved against AllowedCommandsPath;
// the FIRST match wins.
func (c Config) resolveCommand(command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("command is empty")
	}
	if filepath.IsAbs(command) {
		if _, err := os.Stat(command); err != nil {
			return "", fmt.Errorf("absolute command %q not found: %w", command, err)
		}
		return command, nil
	}
	for _, dir := range c.AllowedCommandsPath {
		candidate := filepath.Join(dir, command)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("command %q not found in AllowedCommandsPath %v", command, c.AllowedCommandsPath)
}

// resolveWorkingDir returns the absolute working dir. Empty input is
// accepted and returns the empty string (caller's cwd is used by
// exec.Cmd). Non-empty input must be absolute AND within one of
// AllowedWorkingDirs.
func (c Config) resolveWorkingDir(wd string) (string, error) {
	if wd == "" {
		return "", nil
	}
	if !filepath.IsAbs(wd) {
		return "", fmt.Errorf("working_dir must be absolute, got %q", wd)
	}
	abs, err := filepath.Abs(wd)
	if err != nil {
		return "", fmt.Errorf("working_dir abs: %w", err)
	}
	for _, root := range c.AllowedWorkingDirs {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if abs == absRoot || strings.HasPrefix(abs, absRoot+string(filepath.Separator)) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("working_dir %q not within AllowedWorkingDirs %v", abs, c.AllowedWorkingDirs)
}

// buildEnv constructs the child process env: baseEnvKeys from the
// current process + caller-provided overrides. Entries with empty
// values are preserved (allows caller to explicitly blank an env var).
func (c Config) buildEnv(callerEnv map[string]string) []string {
	merged := map[string]string{}
	for _, k := range baseEnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			merged[k] = v
		}
	}
	for k, v := range callerEnv {
		merged[k] = v
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	return out
}
