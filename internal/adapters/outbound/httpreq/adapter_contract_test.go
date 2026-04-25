package httpreq_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/httpreq"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/test/contract"
)

func TestHTTPAdapter_ContractSuite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	a, err := httpreq.NewAdapter(httpreq.Config{
		AllowPrivateNetworks: true, // httptest binds to 127.0.0.1
		InlineStreamLimit:    16 * 1024,
	}, &shared.FakeClock{T: time.Now()})
	if err != nil {
		t.Fatal(err)
	}

	contract.RunContractSuite(t, a, map[string]contract.SamplePayload{
		"http.request@v1": {
			Valid: jsonHTTP(t, map[string]any{"method": "GET", "url": srv.URL}),
			Invalid: []json.RawMessage{
				jsonHTTP(t, map[string]any{}),                                           // missing method+url
				jsonHTTP(t, map[string]any{"method": "GET"}),                            // missing url
				jsonHTTP(t, map[string]any{"method": "GET", "url": "ftp://x.com"}),      // bad scheme
				jsonHTTP(t, map[string]any{"method": "GET", "url": srv.URL, "junk": 1}), // unknown field
				json.RawMessage(`not json`),
			},
		},
	})
}

func jsonHTTP(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
