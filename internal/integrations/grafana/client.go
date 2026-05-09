package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Sentinel errors per D2C4D.12.
//   - ErrGrafanaClient5xx: server-side, transport, or 429 — treat as
//     transient; the handler responds 502/503 to alertmanager so it
//     retries per its notifier retry/backoff behavior.
//   - ErrGrafanaClient4xx: non-transient client error (400/401/403/404)
//     — the handler responds 204 to alertmanager (NO retry; documented
//     deliberate divergence from linear-webhook per spec §4.3).
//
// Production code paths return *ClientError values that wrap one of
// these sentinels, exposing the HTTP status code for structured
// logging (A2C4D.4).
var (
	ErrGrafanaClient5xx = errors.New("grafana client: server / transport / 429")
	ErrGrafanaClient4xx = errors.New("grafana client: client error 4xx")
)

// ClientError is the typed error returned by HTTPGrafanaClient. It
// carries the actual HTTP StatusCode (or 0 for transport / marshal
// errors) so the handler can include it in structured 4xx error
// logs per A2C4D.4. errors.Is(err, ErrGrafanaClient4xx) and
// errors.Is(err, ErrGrafanaClient5xx) both work for policy.
type ClientError struct {
	StatusCode int   // HTTP status (0 for transport / marshal failures)
	Sentinel   error // ErrGrafanaClient4xx | ErrGrafanaClient5xx
	Cause      error // optional underlying cause (transport, marshal)
}

// Error formats the error.
func (e *ClientError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%v: status %d: %v", e.Sentinel, e.StatusCode, e.Cause)
	}
	return fmt.Sprintf("%v: status %d", e.Sentinel, e.StatusCode)
}

// Is supports errors.Is(err, ErrGrafanaClient4xx/5xx).
func (e *ClientError) Is(target error) bool {
	return errors.Is(e.Sentinel, target)
}

// Unwrap exposes the underlying transport / marshal cause for further
// inspection (e.g. log.Error(... slog.Any("cause", err))).
func (e *ClientError) Unwrap() error {
	return e.Cause
}

// GrafanaClient is the minimal interface (D2C4D.7 — only the
// PostAnnotation surface is needed; if the adapter ever grows
// additional Grafana endpoints, extend the interface).
type GrafanaClient interface {
	PostAnnotation(ctx context.Context, req AnnotationRequest) error
}

// HTTPGrafanaClient is the production GrafanaClient implementation.
// It posts to <baseURL>/api/annotations with Authorization: Bearer
// <token>.
type HTTPGrafanaClient struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewHTTPGrafanaClient constructs a client. baseURL is the Grafana
// instance root (no trailing slash). client may be nil — defaults
// to http.DefaultClient.
func NewHTTPGrafanaClient(baseURL, token string, client *http.Client) *HTTPGrafanaClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPGrafanaClient{
		baseURL: baseURL,
		token:   token,
		client:  client,
	}
}

// PostAnnotation issues POST <baseURL>/api/annotations with the
// supplied request body. Status mapping per D2C4D.12; returns
// *ClientError on failure (A2C4D.4).
func (c *HTTPGrafanaClient) PostAnnotation(ctx context.Context, req AnnotationRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		// Local marshal error — caller can't retry meaningfully; treat as 4xx.
		return &ClientError{StatusCode: 0, Sentinel: ErrGrafanaClient4xx, Cause: fmt.Errorf("marshal: %w", err)}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/annotations", bytes.NewReader(body))
	if err != nil {
		return &ClientError{StatusCode: 0, Sentinel: ErrGrafanaClient5xx, Cause: fmt.Errorf("build request: %w", err)}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		// Transport error — treat as transient.
		return &ClientError{StatusCode: 0, Sentinel: ErrGrafanaClient5xx, Cause: fmt.Errorf("transport: %w", err)}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body) // drain so the conn is reusable

	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return &ClientError{StatusCode: resp.StatusCode, Sentinel: ErrGrafanaClient5xx}
	case resp.StatusCode >= 400:
		return &ClientError{StatusCode: resp.StatusCode, Sentinel: ErrGrafanaClient4xx}
	default:
		return &ClientError{StatusCode: resp.StatusCode, Sentinel: ErrGrafanaClient5xx, Cause: fmt.Errorf("unexpected status")}
	}
}
