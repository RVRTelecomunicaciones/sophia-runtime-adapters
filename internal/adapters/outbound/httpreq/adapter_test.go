package httpreq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

func TestNewAdapter_Validation(t *testing.T) {
	clk := &shared.FakeClock{T: time.Now()}

	t.Run("zero InlineStreamLimit", func(t *testing.T) {
		_, err := NewAdapter(Config{}, clk)
		if err == nil {
			t.Fatal("expected error for zero InlineStreamLimit")
		}
	})
	t.Run("nil Clock", func(t *testing.T) {
		_, err := NewAdapter(Config{InlineStreamLimit: 1024}, nil)
		if err == nil {
			t.Fatal("expected error for nil clock")
		}
	})
	t.Run("valid config", func(t *testing.T) {
		a, err := NewAdapter(Config{InlineStreamLimit: 1024}, clk)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a == nil {
			t.Fatal("expected non-nil adapter")
		}
	})
}

func TestAdapter_ID(t *testing.T) {
	a := newTestHTTPAdapter(t)
	if got := a.ID().String(); got != "http" {
		t.Errorf("ID() = %q, want %q", got, "http")
	}
}

func TestAdapter_Capabilities(t *testing.T) {
	a := newTestHTTPAdapter(t)
	caps := a.Capabilities()
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	if got := caps[0].Canonical(); got != "http.request@v1" {
		t.Errorf("canonical = %q, want %q", got, "http.request@v1")
	}
	// Stable across calls.
	caps2 := a.Capabilities()
	if len(caps2) != 1 || caps2[0].Canonical() != caps[0].Canonical() {
		t.Error("Capabilities() not stable across calls")
	}
	// Each capability's AdapterID matches the adapter's own ID.
	id := a.ID()
	for _, c := range caps {
		if c.AdapterID() != id {
			t.Errorf("capability adapter_id %q != adapter ID %q", c.AdapterID().String(), id.String())
		}
	}
}

func TestAdapter_Execute_UnknownCapability(t *testing.T) {
	a := newTestHTTPAdapter(t)

	id, _ := valueobjects.NewAdapterID("http")
	fakeCap, _ := valueobjects.NewCapability(id, "unknown_op", "v1", false, 5*time.Second)

	pl, _ := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(`{}`), 0)
	raw, err := a.Execute(context.Background(), fakeCap, pl)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if raw == nil {
		t.Fatal("expected non-nil raw for unknown capability")
	}
	r := raw.(*httpRaw)
	if r.validation == "" {
		t.Error("expected validation error for unknown capability")
	}
}

func TestAdapter_Execute_CancelledCtxBeforeDispatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	cap := a.Capabilities()[0]

	b, _ := json.Marshal(map[string]any{"method": "GET", "url": srv.URL})
	pl, _ := valueobjects.NewPayload(valueobjects.ContentTypeJSON, b, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	raw, err := a.Execute(ctx, cap, pl)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if raw == nil {
		t.Fatal("expected non-nil raw for cancelled ctx")
	}
	r := raw.(*httpRaw)
	if r.ctxErr == nil {
		t.Error("expected ctxErr for pre-cancelled context")
	}
}
