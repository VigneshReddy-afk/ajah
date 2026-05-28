package flagging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func newTestFlagger(t *testing.T, scorerURL string) *Flagger {
	t.Helper()
	return New(scorerURL, zap.NewNop())
}

func mockScorer(hallucination, grounding float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"request_id":               "test",
			"hallucination_score":      hallucination,
			"factual_consistency_score": grounding,
			"toxicity_score":           0.0,
			"overall_quality_score":    0.5,
			"flags":                    []string{},
			"processing_ms":            10,
		})
	}))
}

func TestEvaluate_HighRiskOnLowGrounding(t *testing.T) {
	srv := mockScorer(0.2, 0.25) // grounding < 0.3 → high
	defer srv.Close()

	f := newTestFlagger(t, srv.URL)
	flag := f.Evaluate(context.Background(), "req-1", "sess-1", "prompt", "response", 0.5)

	if flag.RiskLevel != "high" {
		t.Errorf("expected RiskLevel=high, got %q", flag.RiskLevel)
	}
	if !flag.ShouldWarn {
		t.Error("expected ShouldWarn=true for high risk")
	}
	if flag.GroundingScore != 0.25 {
		t.Errorf("expected GroundingScore=0.25, got %f", flag.GroundingScore)
	}
	if flag.HallucinationRisk != 0.2 {
		t.Errorf("expected HallucinationRisk=0.2, got %f", flag.HallucinationRisk)
	}
	if len(flag.Reasons) == 0 {
		t.Fatal("expected at least one reason")
	}
	if !containsSubstr(flag.Reasons, "weak grounding") {
		t.Errorf("expected grounding reason, got %v", flag.Reasons)
	}
}

func TestEvaluate_HighRiskOnHighHallucination(t *testing.T) {
	srv := mockScorer(0.82, 0.65) // hallucination > 0.7 → high
	defer srv.Close()

	f := newTestFlagger(t, srv.URL)
	flag := f.Evaluate(context.Background(), "req-2", "sess-2", "prompt", "response", 0.4)

	if flag.RiskLevel != "high" {
		t.Errorf("expected RiskLevel=high, got %q", flag.RiskLevel)
	}
	if !flag.ShouldWarn {
		t.Error("expected ShouldWarn=true for high risk")
	}
	if flag.HallucinationRisk != 0.82 {
		t.Errorf("expected HallucinationRisk=0.82, got %f", flag.HallucinationRisk)
	}
	if !containsSubstr(flag.Reasons, "hallucination signal") {
		t.Errorf("expected hallucination reason, got %v", flag.Reasons)
	}
}

func TestEvaluate_MediumRisk(t *testing.T) {
	srv := mockScorer(0.55, 0.45) // hallucination in [0.4,0.7] and grounding in [0.3,0.5] → medium
	defer srv.Close()

	f := newTestFlagger(t, srv.URL)
	flag := f.Evaluate(context.Background(), "req-3", "sess-3", "prompt", "response", 0.5)

	if flag.RiskLevel != "medium" {
		t.Errorf("expected RiskLevel=medium, got %q", flag.RiskLevel)
	}
	if !flag.ShouldWarn {
		t.Error("expected ShouldWarn=true for medium risk")
	}
	if !containsSubstr(flag.Reasons, "weak grounding") {
		t.Errorf("expected grounding reason, got %v", flag.Reasons)
	}
	if !containsSubstr(flag.Reasons, "hallucination signal") {
		t.Errorf("expected hallucination reason, got %v", flag.Reasons)
	}
}

func TestEvaluate_LowRisk(t *testing.T) {
	srv := mockScorer(0.1, 0.90) // both fine → low
	defer srv.Close()

	f := newTestFlagger(t, srv.URL)
	flag := f.Evaluate(context.Background(), "req-4", "sess-4", "prompt", "response", 0.9)

	if flag.RiskLevel != "low" {
		t.Errorf("expected RiskLevel=low, got %q", flag.RiskLevel)
	}
	if flag.ShouldWarn {
		t.Error("expected ShouldWarn=false for low risk")
	}
	if len(flag.Reasons) != 1 {
		t.Fatalf("expected exactly one reason, got %d: %v", len(flag.Reasons), flag.Reasons)
	}
	if !strings.Contains(flag.Reasons[0], "well-grounded") {
		t.Errorf("expected positive reason, got %q", flag.Reasons[0])
	}
}

func TestEvaluate_ScorerUnavailableReturnsUnknown(t *testing.T) {
	// Point at a dead address — no server running there.
	f := newTestFlagger(t, "http://127.0.0.1:19999")
	flag := f.Evaluate(context.Background(), "req-5", "sess-5", "prompt", "response", 0.0)

	if flag.RiskLevel != "unknown" {
		t.Errorf("expected RiskLevel=unknown, got %q", flag.RiskLevel)
	}
	if flag.ShouldWarn {
		t.Error("expected ShouldWarn=false when scorer unavailable")
	}
	if len(flag.Reasons) != 1 || flag.Reasons[0] != "scoring unavailable" {
		t.Errorf("expected [\"scoring unavailable\"], got %v", flag.Reasons)
	}
	// RequestID and SessionID must still be populated.
	if flag.RequestID != "req-5" {
		t.Errorf("expected RequestID=req-5, got %q", flag.RequestID)
	}
	if flag.SessionID != "sess-5" {
		t.Errorf("expected SessionID=sess-5, got %q", flag.SessionID)
	}
}

// containsSubstr returns true if any element of reasons contains substr.
func containsSubstr(reasons []string, substr string) bool {
	for _, r := range reasons {
		if strings.Contains(r, substr) {
			return true
		}
	}
	return false
}
