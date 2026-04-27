package httpreq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/chaos"
)

// SupportedChaosFaults returns the six adapter-synthetic fault kinds supported
// on http.request@v1 per spec Appendix D. Returns nil for any other capability.
func (a *Adapter) SupportedChaosFaults(cap valueobjects.Capability) []chaos.FaultKind {
	if cap.Canonical() != "http.request@v1" {
		return nil
	}
	return []chaos.FaultKind{
		chaos.FaultConnectionReset,
		chaos.FaultSlowBody,
		chaos.FaultRedirectLoop,
		chaos.FaultTLSHandshakeFail,
		chaos.FaultAuthFailure,
		chaos.FaultRemoteUnreachable,
	}
}

// SyntheticOutcome constructs an *httpRaw of the same shape the adapter would
// produce for the equivalent real fault, then returns it without executing any
// real side effects. The normalizer's priority-3 (runErr) or priority-5
// (status-mismatch) branch classifies each fault as failure / ExternalFailure.
//
// URL is decoded from the payload for realistic observability per I24; falls
// back to "<unknown>" on decode failure (R4 defensive — chaos must never panic
// on malformed payloads).
func (a *Adapter) SyntheticOutcome(
	_ context.Context,
	cap valueobjects.Capability,
	payload valueobjects.Payload,
	fault chaos.FaultConfig,
) (services.AdapterRawOutcome, bool) {
	if cap.Canonical() != "http.request@v1" {
		return nil, false
	}

	url, expectedStatus := decodeRequestForChaos(payload)

	switch fault.Kind {
	case chaos.FaultConnectionReset:
		return &httpRaw{
			runErr: errors.New("connection reset by peer: " + url),
		}, true

	case chaos.FaultSlowBody:
		return &httpRaw{
			runErr: errors.New("slow body read: deadline exceeded for " + url),
		}, true

	case chaos.FaultRedirectLoop:
		maxRedirects := a.cfg.MaxRedirects
		if maxRedirects <= 0 {
			maxRedirects = defaultMaxRedirects
		}
		return &httpRaw{
			runErr:        fmt.Errorf("stopped after maximum redirects (%d) for %s", maxRedirects, url),
			redirectCount: maxRedirects + 1,
		}, true

	case chaos.FaultTLSHandshakeFail:
		return &httpRaw{
			runErr: errors.New("tls: handshake failure for " + url),
		}, true

	case chaos.FaultAuthFailure:
		// Use the caller's expected_status if supplied; default to [200] so the
		// status-mismatch branch fires even when the payload omits expected_status.
		es := expectedStatus
		if len(es) == 0 {
			es = []int{200}
		}
		return &httpRaw{
			statusCode:     401,
			expectedStatus: es,
		}, true

	case chaos.FaultRemoteUnreachable:
		return &httpRaw{
			runErr: errors.New("dial tcp: no route to host: " + url),
		}, true

	default:
		return nil, false
	}
}

// decodeRequestForChaos tolerantly extracts URL and ExpectedStatus from the
// payload. Returns "<unknown>" / nil on decode failure (R4 — chaos must never
// panic on malformed payloads; produces best-effort realistic shapes per I24).
func decodeRequestForChaos(payload valueobjects.Payload) (url string, expectedStatus []int) {
	url = "<unknown>"
	if raw := payload.Raw(); len(raw) > 0 {
		var p RequestPayload
		if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&p); err == nil {
			if p.URL != "" {
				url = p.URL
			}
			expectedStatus = p.ExpectedStatus
		}
	}
	return url, expectedStatus
}

// Compile-time assertion: *Adapter must satisfy chaos.ChaosCapable.
var _ chaos.ChaosCapable = (*Adapter)(nil)
