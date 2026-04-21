package filesystem

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

func (a *Adapter) writeFile(ctx context.Context, payload valueobjects.Payload) (services.AdapterRawOutcome, error) {
	t0 := a.clk.Now()

	var p WriteFilePayload
	if err := decodeStrict(payload, &p); err != nil {
		return &writeRaw{validation: fmt.Sprintf("decode payload: %v", err), durationMs: a.elapsedMs(t0)}, nil
	}
	if p.Path == "" {
		return &writeRaw{validation: "path is required", durationMs: a.elapsedMs(t0)}, nil
	}
	mode := p.Mode
	if mode == 0 {
		mode = 0o600
	}

	// Determine target path. If create_dirs, ensure parent exists first
	// (then resolve). Otherwise resolve directly.
	if p.CreateDirs {
		dir := filepath.Dir(p.Path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return &writeRaw{runErr: fmt.Errorf("mkdir parents: %w", err), durationMs: a.elapsedMs(t0)}, nil
		}
	}
	resolved, err := a.cfg.resolveTargetPath(p.Path)
	if err != nil {
		return &writeRaw{validation: err.Error(), durationMs: a.elapsedMs(t0)}, nil
	}

	if !p.Overwrite {
		if _, err := os.Stat(resolved); err == nil {
			return &writeRaw{validation: fmt.Sprintf("target %q exists and overwrite=false", resolved), durationMs: a.elapsedMs(t0)}, nil
		}
	}

	if err := ctx.Err(); err != nil {
		return &writeRaw{ctxErr: err, durationMs: a.elapsedMs(t0)}, nil
	}

	// Atomic: write tmp file, fsync, rename.
	tmpName := resolved + ".tmp." + randomSuffix(12)
	tmp, err := os.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(mode)) //nolint:gosec
	if err != nil {
		return &writeRaw{runErr: fmt.Errorf("create tmp: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}
	if _, err := tmp.Write(p.Data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return &writeRaw{runErr: fmt.Errorf("write tmp: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return &writeRaw{runErr: fmt.Errorf("fsync tmp: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return &writeRaw{runErr: fmt.Errorf("close tmp: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}
	if err := os.Rename(tmpName, resolved); err != nil {
		_ = os.Remove(tmpName)
		return &writeRaw{runErr: fmt.Errorf("rename: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}

	sum := sha256.Sum256(p.Data)
	return &writeRaw{
		resolvedPath: resolved,
		bytesWritten: int64(len(p.Data)),
		checksumHex:  hex.EncodeToString(sum[:]),
		mode:         mode,
		durationMs:   a.elapsedMs(t0),
	}, nil
}

func randomSuffix(n int) string {
	b := make([]byte, n/2+1)
	_, _ = io.ReadFull(rand.Reader, b)
	return hex.EncodeToString(b)[:n]
}
