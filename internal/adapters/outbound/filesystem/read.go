package filesystem

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

func (a *Adapter) readFile(ctx context.Context, payload valueobjects.Payload) (services.AdapterRawOutcome, error) {
	t0 := a.clk.Now()
	const defaultMaxBytes = 1 << 20 // 1 MiB

	var p ReadFilePayload
	if err := decodeStrict(payload, &p); err != nil {
		return &readRaw{validation: fmt.Sprintf("decode payload: %v", err), durationMs: a.elapsedMs(t0)}, nil
	}
	if p.Path == "" {
		return &readRaw{validation: "path is required", durationMs: a.elapsedMs(t0)}, nil
	}
	resolved, err := a.cfg.resolveExistingPath(p.Path)
	if err != nil {
		return &readRaw{validation: err.Error(), durationMs: a.elapsedMs(t0)}, nil
	}

	maxBytes := p.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}

	if err := ctx.Err(); err != nil {
		return &readRaw{ctxErr: err, durationMs: a.elapsedMs(t0)}, nil
	}

	f, err := os.Open(resolved) //nolint:gosec // path is allowlist-validated above
	if err != nil {
		return &readRaw{runErr: fmt.Errorf("open: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return &readRaw{runErr: fmt.Errorf("stat: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}
	totalBytes := info.Size()

	// Read up to maxBytes+1 to detect oversize.
	limit := int64(maxBytes) + 1
	r := io.LimitReader(f, limit)
	data, err := io.ReadAll(r)
	if err != nil {
		return &readRaw{runErr: fmt.Errorf("read: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}

	truncated := false
	if int64(len(data)) > int64(maxBytes) {
		data = data[:maxBytes]
		truncated = true
	}

	return &readRaw{
		data:       data,
		totalBytes: totalBytes,
		truncated:  truncated,
		durationMs: a.elapsedMs(t0),
	}, nil
}
