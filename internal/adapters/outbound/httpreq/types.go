// Package httpreq implements the Phase 1 HTTP adapter (http.request@v1)
// per spec §8.5. External AdapterID is "http" (Gap-1 resolution); the
// Go package name "httpreq" is internal-only and avoids stdlib net/http
// shadowing at import sites.
//
// Safety defaults: SSRF strict (private IP ranges blocked), TLS verify
// mandatory (no InsecureSkipVerify), redirect chain capped at 5,
// http/https schemes only.
package httpreq

// Config bundles bootstrap-loaded HTTP adapter settings.
type Config struct {
	// AllowPrivateNetworks disables the SSRF private-IP block (A8.3).
	// Default false (strict). Operators set true only for known-safe
	// internal pilot scenarios.
	AllowPrivateNetworks bool

	// HostAllowlist (optional, log-only in Phase 1 per §8.5). When
	// non-empty, requests to hosts NOT in this list emit a warning in
	// adapter_meta but are NOT rejected. Pre-Phase-2-hardening.
	HostAllowlist []string

	// MaxBodyBytes caps response body reads. Default 10 MiB.
	MaxBodyBytes int

	// MaxRedirects caps the redirect chain. Default 5.
	MaxRedirects int

	// InlineStreamLimit caps stdout_ref (response body) bytes when
	// constructing entities.StreamRef.
	InlineStreamLimit int
}

// RequestPayload — wire payload for http.request@v1.
type RequestPayload struct {
	Method         string            `json:"method"` // GET/POST/PUT/DELETE/PATCH/HEAD/OPTIONS
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers,omitempty"`
	Body           []byte            `json:"body,omitempty"`            // base64 on wire
	ExpectedStatus []int             `json:"expected_status,omitempty"` // overrides default 2xx
	MaxBodyBytes   int               `json:"max_body_bytes,omitempty"`  // overrides config
	MaxRedirects   int               `json:"max_redirects,omitempty"`   // overrides config
}

type httpRaw struct {
	validation     string
	statusCode     int
	headers        map[string]string
	body           []byte
	bodyTruncated  bool
	totalBytes     int64
	redirectCount  int
	durationMs     int64
	ctxErr         error
	runErr         error
	expectedStatus []int
}

func (*httpRaw) IsAdapterRawOutcome() {}

const (
	defaultMaxBodyBytes = 10 << 20 // 10 MiB
	defaultMaxRedirects = 5
)
