package filesystem

import (
	"fmt"
	"path/filepath"
	"strings"
)

// resolvePath returns the canonical absolute path for p, validating
// that:
//  1. p is non-empty.
//  2. After EvalSymlinks + Abs, the resolved path is within one of
//     Config.AllowedFilesystemRoots.
//
// For WRITE: the file may not exist yet — callers pass the parent
// dir to resolvePath() and let the actual write happen at the
// joined path. We expose resolveExistingPath (for reads) and
// resolveTargetPath (for writes — checks parent dir's resolved root).
func (c Config) resolveExistingPath(p string) (string, error) {
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
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("eval symlinks: %w", err)
	}
	return c.checkInRoots(resolved)
}

// resolveTargetPath validates a path that may not yet exist. It
// resolves the PARENT directory (which must exist) via EvalSymlinks,
// then re-joins the basename. The resolved parent must be within an
// allowed root.
func (c Config) resolveTargetPath(p string) (string, error) {
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
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		// Dir doesn't exist; create_dirs may handle this. Caller
		// branches on create_dirs to decide. Return original abs as
		// "best effort"; allowlist check below uses the unresolved
		// path which is still safe (no symlink-escape since dir
		// doesn't exist to symlink).
		resolvedDir = dir
	}
	if _, err := c.checkInRoots(resolvedDir); err != nil {
		return "", err
	}
	return filepath.Join(resolvedDir, base), nil
}

func (c Config) checkInRoots(abs string) (string, error) {
	for _, root := range c.AllowedFilesystemRoots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		realRoot, err := filepath.EvalSymlinks(absRoot)
		if err != nil {
			realRoot = absRoot
		}
		if abs == realRoot || strings.HasPrefix(abs, realRoot+string(filepath.Separator)) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("path %q not within AllowedFilesystemRoots %v", abs, c.AllowedFilesystemRoots)
}
