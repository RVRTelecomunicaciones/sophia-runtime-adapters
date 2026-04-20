package entities_test

import (
	"fmt"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

func TestPartialClassification_Classify_TruthTable(t *testing.T) {
	cases := []struct {
		allows, arts, unc, reverted bool
		expected                    valueobjects.ExecutionStatus
	}{
		// AllowsPartial=false → always failure (rows 0-7)
		{false, false, false, false, valueobjects.StatusFailure},
		{false, false, false, true, valueobjects.StatusFailure},
		{false, false, true, false, valueobjects.StatusFailure},
		{false, false, true, true, valueobjects.StatusFailure},
		{false, true, false, false, valueobjects.StatusFailure},
		{false, true, false, true, valueobjects.StatusFailure},
		{false, true, true, false, valueobjects.StatusFailure},
		{false, true, true, true, valueobjects.StatusFailure},
		// AllowsPartial=true, HasUncompletedStep=false → success (rows 8-9, 12-13)
		{true, false, false, false, valueobjects.StatusSuccess},
		{true, false, false, true, valueobjects.StatusSuccess},
		// AllowsPartial=true, HasUncompletedStep=true, HasArtifacts=false → failure (rows 10-11)
		{true, false, true, false, valueobjects.StatusFailure},
		{true, false, true, true, valueobjects.StatusFailure},
		// AllowsPartial=true, HasUncompletedStep=false, HasArtifacts=true → success (rows 12-13)
		{true, true, false, false, valueobjects.StatusSuccess},
		{true, true, false, true, valueobjects.StatusSuccess},
		// AllowsPartial=true, HasUncompletedStep=true, HasArtifacts=true, !Reverted → partial (row 14)
		{true, true, true, false, valueobjects.StatusPartial},
		// AllowsPartial=true, HasUncompletedStep=true, HasArtifacts=true, Reverted → failure (row 15)
		{true, true, true, true, valueobjects.StatusFailure},
	}

	boolStr := func(b bool) string {
		if b {
			return "T"
		}
		return "F"
	}

	for i, tc := range cases {
		name := fmt.Sprintf("row%02d_allows=%s_arts=%s_unc=%s_rev=%s",
			i,
			boolStr(tc.allows),
			boolStr(tc.arts),
			boolStr(tc.unc),
			boolStr(tc.reverted),
		)
		t.Run(name, func(t *testing.T) {
			p := entities.PartialClassification{
				AllowsPartial:      tc.allows,
				HasArtifacts:       tc.arts,
				HasUncompletedStep: tc.unc,
				Reverted:           tc.reverted,
			}
			got := p.Classify()
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestPartialClassification_Classify_CanonicalPartial verifies that row 14
// (the sole combination that satisfies the 4-AND rule) is the ONLY input
// that produces StatusPartial.
func TestPartialClassification_Classify_CanonicalPartial(t *testing.T) {
	p := entities.PartialClassification{
		AllowsPartial:      true,
		HasArtifacts:       true,
		HasUncompletedStep: true,
		Reverted:           false,
	}
	got := p.Classify()
	if got != valueobjects.StatusPartial {
		t.Errorf("canonical partial row: got %q, want %q", got, valueobjects.StatusPartial)
	}
}
