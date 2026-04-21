package git

import (
	"fmt"
	"path/filepath"
	"strings"
)

// resolvePath validates that p is absolute and within one of the
// AllowedWorkingDirs. Returns the cleaned absolute path on success.
// Used by status/diff/commit (existing repo path) and clone
// (destination root). Empty p is rejected — this helper assumes a
// non-empty input.
func (c Config) resolvePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("path must be absolute, got %q", p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("path abs: %w", err)
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
	return "", fmt.Errorf("path %q not within AllowedWorkingDirs %v", abs, c.AllowedWorkingDirs)
}
