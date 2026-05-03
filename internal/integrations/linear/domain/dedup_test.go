package domain_test

import (
	"strings"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/domain"
)

func TestDedupLabel_DeterministicForSameGroupKey(t *testing.T) {
	gk := `{}/{severity="critical"}/{alertname="ShellExecLatencyBurnRate",adapter="shell",capability="shell.exec@v1"}`
	a := domain.DedupLabel(gk)
	b := domain.DedupLabel(gk)
	if a != b {
		t.Errorf("DedupLabel not deterministic: %q != %q", a, b)
	}
}

func TestDedupLabel_FormatAlertColonTwelveHexChars(t *testing.T) {
	gk := "anything"
	got := domain.DedupLabel(gk)
	if !strings.HasPrefix(got, "alert:") {
		t.Errorf("DedupLabel = %q, expected prefix 'alert:'", got)
	}
	suffix := strings.TrimPrefix(got, "alert:")
	if len(suffix) != 12 {
		t.Errorf("DedupLabel suffix length = %d, want 12", len(suffix))
	}
	for _, c := range suffix {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("DedupLabel suffix contains non-hex char %q in %q", c, got)
		}
	}
}

func TestDedupLabel_DifferentGroupKeysProduceDifferentLabels(t *testing.T) {
	a := domain.DedupLabel("groupKey-A")
	b := domain.DedupLabel("groupKey-B")
	if a == b {
		t.Errorf("DedupLabel collision on distinct keys: both = %q", a)
	}
}
