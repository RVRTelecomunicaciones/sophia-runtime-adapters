//go:build loki_labels

// Package loki_labels enforces the Loki label cardinality invariant
// (D2C4D.8) at CI time. Run with the `loki_labels` build tag — the
// CI job `loki-label-cardinality-check` sets it.
package loki_labels_test

import (
	"os"
	"strings"
	"testing"
)

// allowedLokiLabels is the closed set of OTel resource attributes
// that may surface as Loki stream labels via either:
//   - distributor.otlp_config.default_resource_attributes_as_index_labels
//   - filelog operator setting resource[<key>] explicitly
//
// Keep this list in sync with spec §3.5 / D2C4D.8.
var allowedLokiLabels = map[string]bool{
	"service.name":      true,
	"service.namespace": true,
	"level":             true,
	"environment":       true,
	"tenant_type":       true,
}

// forbiddenLokiLabels are high-cardinality fields that MUST stay in
// the log line (parsed at query time via `| json`), NEVER as labels.
// If any of these appear in the collector or Loki config in a label-
// promoting context, the test fails loud.
var forbiddenLokiLabels = []string{
	"trace_id",
	"span_id",
	"correlation_id",
	"capability",
	"adapter",
	"error_class",
	"retry_hint",
	"request_id",
	"user_id",
	"project_id",
	"tenant_id",
}

func TestLokiConfig_NoForbiddenLabels(t *testing.T) {
	data, err := os.ReadFile("loki.yaml")
	if err != nil {
		t.Fatalf("read loki.yaml: %v", err)
	}
	content := string(data)

	// Find the default_resource_attributes_as_index_labels block; only
	// keys listed there become Loki stream labels via the OTLP receiver.
	idx := strings.Index(content, "default_resource_attributes_as_index_labels:")
	if idx < 0 {
		t.Fatal("loki.yaml: distributor.otlp_config.default_resource_attributes_as_index_labels block missing — required by D2C4D.4")
	}
	block := content[idx:]

	for _, forbidden := range forbiddenLokiLabels {
		if strings.Contains(block, "- "+forbidden) || strings.Contains(block, ": "+forbidden) {
			t.Errorf("loki.yaml: forbidden label %q found in index_labels block — high cardinality, must stay in log line (D2C4D.8)", forbidden)
		}
	}
}

func TestCollectorConfig_NoForbiddenResourceAttrPromotion(t *testing.T) {
	data, err := os.ReadFile("../otel-collector/config.yaml")
	if err != nil {
		t.Fatalf("read collector config: %v", err)
	}
	content := string(data)

	// Look for `field: resource["<key>"]` patterns in filelog operators.
	// Those promote attribute values to OTel resource attributes (which
	// then become Loki labels via the OTLP receiver). The set of allowed
	// keys is closed by allowedLokiLabels.
	for _, forbidden := range forbiddenLokiLabels {
		needle1 := "resource[\"" + forbidden + "\"]"
		needle2 := "resource['" + forbidden + "']"
		if strings.Contains(content, needle1) || strings.Contains(content, needle2) {
			t.Errorf("collector config: forbidden resource attribute promotion %q — high cardinality, must remain a log-line attribute (D2C4D.8)", forbidden)
		}
	}
}

func TestLokiAllowlist_OnlyExpectedKeys(t *testing.T) {
	// Round-trip sanity: ensure the allowlist matches the spec.
	expected := []string{"service.name", "service.namespace", "level", "environment", "tenant_type"}
	if len(allowedLokiLabels) != len(expected) {
		t.Errorf("allowedLokiLabels: size %d, want %d (spec §3.5)", len(allowedLokiLabels), len(expected))
	}
	for _, k := range expected {
		if !allowedLokiLabels[k] {
			t.Errorf("allowedLokiLabels: missing key %q (spec §3.5)", k)
		}
	}
}
