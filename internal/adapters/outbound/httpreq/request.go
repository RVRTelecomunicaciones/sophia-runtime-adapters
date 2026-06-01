package httpreq

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

func (a *Adapter) request(ctx context.Context, payload valueobjects.Payload) (services.AdapterRawOutcome, error) {
	t0 := a.clk.Now()

	var p RequestPayload
	if err := decodeStrict(payload, &p); err != nil {
		return &httpRaw{validation: fmt.Sprintf("decode payload: %v", err), durationMs: a.elapsedMs(t0)}, nil
	}
	if p.Method == "" {
		return &httpRaw{validation: "method is required", durationMs: a.elapsedMs(t0)}, nil
	}
	if p.URL == "" {
		return &httpRaw{validation: "url is required", durationMs: a.elapsedMs(t0)}, nil
	}
	if err := a.cfg.validateURL(p.URL, nil); err != nil {
		return &httpRaw{validation: err.Error(), durationMs: a.elapsedMs(t0)}, nil
	}

	maxBody := p.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = a.cfg.MaxBodyBytes
	}
	maxRedirects := p.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = a.cfg.MaxRedirects
	}

	var body io.Reader
	if len(p.Body) > 0 {
		body = bytes.NewReader(p.Body)
	}

	req, err := http.NewRequestWithContext(ctx, p.Method, p.URL, body)
	if err != nil {
		return &httpRaw{validation: fmt.Sprintf("build request: %v", err), durationMs: a.elapsedMs(t0)}, nil
	}
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}

	redirectCount := 0
	client := &http.Client{
		// TLS verify mandatory: rely on default Transport (no override).
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirectCount = len(via)
			if len(via) >= maxRedirects {
				return fmt.Errorf("max redirects %d exceeded", maxRedirects)
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &httpRaw{ctxErr: ctx.Err(), durationMs: a.elapsedMs(t0)}, nil
		}
		return &httpRaw{runErr: err, durationMs: a.elapsedMs(t0)}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	// Read up to maxBody+1 to detect truncation.
	limited := io.LimitReader(resp.Body, int64(maxBody)+1)
	bodyBytes, readErr := io.ReadAll(limited)
	if readErr != nil {
		return &httpRaw{
			runErr:        fmt.Errorf("read body: %w", readErr),
			statusCode:    resp.StatusCode,
			redirectCount: redirectCount,
			durationMs:    a.elapsedMs(t0),
		}, nil
	}
	totalBytes := int64(len(bodyBytes))
	truncated := false
	if totalBytes > int64(maxBody) {
		// Body exceeded limit — truncate retained bytes and count the rest
		// by draining the remaining response body.
		bodyBytes = bodyBytes[:maxBody]
		truncated = true
		// Count remaining bytes so total_bytes reflects full response size.
		remaining, _ := io.Copy(io.Discard, resp.Body)
		totalBytes = int64(maxBody) + 1 + remaining
	}

	headers := map[string]string{}
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	return &httpRaw{
		statusCode:     resp.StatusCode,
		headers:        headers,
		body:           bodyBytes,
		bodyTruncated:  truncated,
		totalBytes:     totalBytes,
		redirectCount:  redirectCount,
		durationMs:     a.elapsedMs(t0),
		expectedStatus: p.ExpectedStatus,
	}, nil
}
