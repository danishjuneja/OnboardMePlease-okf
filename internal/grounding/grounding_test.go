package grounding

import (
	"context"
	"encoding/json"
	"onboardmeplease/internal/privacy"
	"testing"
)

type reviewer struct {
	calls int
	raw   string
	input any
}

func (m *reviewer) JSON(_ context.Context, _ privacy.ModelRequest, _ string, input any, _ json.RawMessage) ([]byte, error) {
	m.calls++
	m.input = input
	return []byte(m.raw), nil
}

func TestOverviewReviewReceivesApplicationCoverage(t *testing.T) {
	model := &reviewer{raw: `{"assessments":[{"index":0,"status":"supported","reason":"Code and application coverage agree"}]}`}
	counts := Coverage{TotalChunks: 132, ReviewedChunks: 91, TotalUnits: 16, ReviewedUnits: 10}
	_, err := Assess(context.Background(), model, privacy.ModelRequest{}, Result{Gaps: []string{}, Claims: []Claim{{Text: "Within 91 of 132 reviewed chunks, the API answers repository questions.", EvidenceIDs: []string{"api"}}}}, []Source{{ID: "api"}}, counts)
	if err != nil {
		t.Fatal(err)
	}
	if model.input.(map[string]any)["application_coverage"] != counts {
		t.Fatal("support checker lost application coverage used in the purpose claim")
	}
}

func TestAssessmentRejectsUnknownIDsUnsupportedAndMissingReviews(t *testing.T) {
	model := &reviewer{raw: `{"assessments":[{"index":0,"status":"unsupported","reason":"Persistence is a stub"}]}`}
	result, err := Assess(context.Background(), model, privacy.ModelRequest{}, Result{Gaps: []string{}, Claims: []Claim{
		{Text: "Writes orders to a database", EvidenceIDs: []string{"source"}},
		{Text: "A second claim", EvidenceIDs: []string{"source"}},
		{Text: "Invented citation", EvidenceIDs: []string{"unknown"}},
	}}, []Source{{ID: "source", Content: "func saveOrderState() error { return nil }"}})
	if err != nil || len(result.Claims) != 0 || result.Rejected != 3 || model.calls != 1 {
		t.Fatalf("assessment: %+v, %v", result, err)
	}
}

func TestAssessmentRejectsDuplicateAndOutOfRangeIndices(t *testing.T) {
	for _, raw := range []string{`{"assessments":[{"index":3,"status":"supported","reason":"x"}]}`, `{"assessments":[{"index":0,"status":"supported","reason":"x"},{"index":0,"status":"supported","reason":"x"}]}`} {
		_, err := Assess(context.Background(), &reviewer{raw: raw}, privacy.ModelRequest{}, Result{Gaps: []string{}, Claims: []Claim{{Text: "Claim", EvidenceIDs: []string{"source"}}}}, []Source{{ID: "source"}})
		if err == nil {
			t.Fatal("invalid support mapping accepted")
		}
	}
}

func TestModelCitationMarkersCannotBecomeRenderedCitations(t *testing.T) {
	got := cleanClaimText("Reads orders[id].[^invented] Calls save.[0123456789abcdef0123456789abcdef]")
	if got != "Reads orders[id]. Calls save." {
		t.Fatal(got)
	}
}
