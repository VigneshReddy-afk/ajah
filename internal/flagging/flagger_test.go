package flagging

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func newTestFlagger(t *testing.T) *Flagger {
	t.Helper()
	return New(zap.NewNop())
}

func TestEvaluate_HighRiskOnLowGrounding(t *testing.T) {
	f := newTestFlagger(t)
	flag := f.Evaluate(context.Background(), "req-1", "sess-1", 0.2, 0.25, 0.0, nil, "")

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
	f := newTestFlagger(t)
	flag := f.Evaluate(context.Background(), "req-2", "sess-2", 0.82, 0.65, 0.0, nil, "")

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
	f := newTestFlagger(t)
	flag := f.Evaluate(context.Background(), "req-3", "sess-3", 0.55, 0.45, 0.0, nil, "")

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
	f := newTestFlagger(t)
	flag := f.Evaluate(context.Background(), "req-4", "sess-4", 0.1, 0.90, 0.0, nil, "")

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

func TestEvaluate_ClaimDensityFlagAddsReason(t *testing.T) {
	f := newTestFlagger(t)
	flags := []string{"high_claim_density"}
	flag := f.Evaluate(context.Background(), "req-5", "sess-5", 0.55, 0.45, 0.75, flags, "")

	if !containsSubstr(flag.Reasons, "High claim density detected") {
		t.Errorf("expected claim density reason, got %v", flag.Reasons)
	}
}

func TestEvaluate_ToxicityFlagAddsReason(t *testing.T) {
	f := newTestFlagger(t)
	flags := []string{"toxicity_detected"}
	flag := f.Evaluate(context.Background(), "req-6", "sess-6", 0.55, 0.45, 0.0, flags, "")

	if !containsSubstr(flag.Reasons, "Toxic or harmful content") {
		t.Errorf("expected toxicity reason, got %v", flag.Reasons)
	}
}

func TestEvaluate_RequestIDPreserved(t *testing.T) {
	f := newTestFlagger(t)
	flag := f.Evaluate(context.Background(), "req-7", "sess-7", 0.1, 0.9, 0.0, nil, "")
	if flag.RequestID != "req-7" {
		t.Errorf("expected RequestID=req-7, got %q", flag.RequestID)
	}
	if flag.SessionID != "sess-7" {
		t.Errorf("expected SessionID=sess-7, got %q", flag.SessionID)
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
