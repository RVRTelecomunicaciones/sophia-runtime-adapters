package filesystem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/chaos"
)

// SupportedChaosFaults reports the adapter-synthetic chaos faults this
// adapter can produce for the given capability. Decorator-native faults
// (latency, hang_until_cancel, inject_panic) are universally available
// across every adapter and are not listed here.
//
// filesystem.read_file@v1 supports eio and permission_denied because
// those map directly to the syscall-layer errors that can surface from
// os.Open / io.ReadFull on a live filesystem.
//
// filesystem.write_file@v1 additionally supports enospc because the
// write path (tmp+fsync+rename) can fail with ENOSPC during the write
// phase before the rename.
//
// Implements chaos.ChaosCapable.
func (a *Adapter) SupportedChaosFaults(cap valueobjects.Capability) []chaos.FaultKind {
	switch cap.Canonical() {
	case "filesystem.read_file@v1":
		return []chaos.FaultKind{
			chaos.FaultEIO,
			chaos.FaultPermissionDenied,
		}
	case "filesystem.write_file@v1":
		return []chaos.FaultKind{
			chaos.FaultEIO,
			chaos.FaultENOSPC,
			chaos.FaultPermissionDenied,
		}
	default:
		return nil
	}
}

// SyntheticOutcome constructs a *readRaw or *writeRaw shape-identical to
// what the real adapter would produce under the equivalent real fault.
// The path is decoded from the payload for realistic observability per I24;
// decode failure falls back to "<unknown>" (R4: must not panic).
//
// Only filesystem.read_file@v1 and filesystem.write_file@v1 support
// adapter-synthetic outcomes; all other capabilities return (nil, false).
//
// Implements chaos.ChaosCapable.
func (a *Adapter) SyntheticOutcome(
	_ context.Context,
	cap valueobjects.Capability,
	payload valueobjects.Payload,
	fault chaos.FaultConfig,
) (services.AdapterRawOutcome, bool) {
	switch cap.Canonical() {
	case "filesystem.read_file@v1":
		path := decodeReadPath(payload)
		switch fault.Kind {
		case chaos.FaultEIO:
			return &readRaw{runErr: errors.New("input/output error: " + path)}, true
		case chaos.FaultPermissionDenied:
			return &readRaw{runErr: errors.New("permission denied: " + path)}, true
		default:
			return nil, false
		}

	case "filesystem.write_file@v1":
		path := decodeWritePath(payload)
		switch fault.Kind {
		case chaos.FaultEIO:
			return &writeRaw{resolvedPath: path, runErr: errors.New("input/output error: " + path)}, true
		case chaos.FaultENOSPC:
			return &writeRaw{resolvedPath: path, runErr: errors.New("no space left on device: " + path)}, true
		case chaos.FaultPermissionDenied:
			return &writeRaw{resolvedPath: path, runErr: errors.New("permission denied: " + path)}, true
		default:
			return nil, false
		}

	default:
		return nil, false
	}
}

func decodeReadPath(payload valueobjects.Payload) string {
	if raw := payload.Raw(); len(raw) > 0 {
		var p ReadFilePayload
		if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&p); err == nil && p.Path != "" {
			return p.Path
		}
	}
	return "<unknown>"
}

func decodeWritePath(payload valueobjects.Payload) string {
	if raw := payload.Raw(); len(raw) > 0 {
		var p WriteFilePayload
		if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&p); err == nil && p.Path != "" {
			return p.Path
		}
	}
	return "<unknown>"
}

// Compile-time assertion that *Adapter implements chaos.ChaosCapable.
var _ chaos.ChaosCapable = (*Adapter)(nil)
