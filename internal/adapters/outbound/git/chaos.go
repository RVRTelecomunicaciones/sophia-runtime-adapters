package git

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
// git.clone@v1 supports remote_unreachable and auth_failure because the
// clone operation makes a real network round-trip; the other git
// capabilities (status, diff, commit) are entirely local — only
// decorator-native faults apply to them.
//
// Implements chaos.ChaosCapable.
func (a *Adapter) SupportedChaosFaults(cap valueobjects.Capability) []chaos.FaultKind {
	switch cap.Canonical() {
	case "git.clone@v1":
		return []chaos.FaultKind{
			chaos.FaultRemoteUnreachable,
			chaos.FaultAuthFailure,
		}
	default:
		// git.status@v1, git.diff@v1, git.commit@v1: local-only,
		// only decorator-native faults apply.
		return nil
	}
}

// SyntheticOutcome constructs a *cloneRaw shape-identical to what the
// real adapter would produce under the equivalent real fault.
// Implementations may inspect payload fields they would use in a real
// execution to produce realistic shapes per I24; they MUST NOT execute
// any real side effects.
//
// Only git.clone@v1 supports adapter-synthetic outcomes; all other
// capabilities return (nil, false).
//
// Implements chaos.ChaosCapable.
func (a *Adapter) SyntheticOutcome(
	_ context.Context,
	cap valueobjects.Capability,
	payload valueobjects.Payload,
	fault chaos.FaultConfig,
) (services.AdapterRawOutcome, bool) {
	if cap.Canonical() != "git.clone@v1" {
		return nil, false
	}

	// Decode the URL for a realistic runErr message; tolerate decode
	// failure gracefully — synthesis must never panic (R4).
	repoURL := "<unknown>"
	if raw := payload.Raw(); len(raw) > 0 {
		var p ClonePayload
		if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&p); err == nil && p.RepoURL != "" {
			repoURL = p.RepoURL
		}
	}

	switch fault.Kind {
	case chaos.FaultRemoteUnreachable:
		// Mirrors the shape produced by go-git when the remote host is
		// unreachable: fetch fails, no filesystem side effects,
		// runErr non-nil, fetchOK false. The normalizer's default/failure
		// branch classifies this as failure / ExternalFailure.
		return &cloneRaw{
			runErr: errors.New("remote unreachable: " + repoURL),
		}, true

	case chaos.FaultAuthFailure:
		// Mirrors the shape produced by go-git for an auth rejection:
		// fetch fails at the authentication step, runErr non-nil,
		// fetchOK false. Normalizer classifies as failure / ExternalFailure.
		return &cloneRaw{
			runErr: errors.New("authentication failed for " + repoURL),
		}, true

	default:
		return nil, false
	}
}

// Compile-time assertion that *Adapter implements chaos.ChaosCapable.
var _ chaos.ChaosCapable = (*Adapter)(nil)
