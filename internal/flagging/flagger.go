package flagging

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// RiskFlag is the evaluated risk output for a single completed LLM response.
type RiskFlag struct {
	RequestID         string   `json:"request_id"`
	SessionID         string   `json:"session_id"`
	HallucinationRisk float64  `json:"hallucination_risk"` // 0.0 to 1.0
	GroundingScore    float64  `json:"grounding_score"`    // 0.0 to 1.0
	RiskLevel         string   `json:"risk_level"`         // "low", "medium", or "high"
	Reasons           []string `json:"reasons"`
	ShouldWarn        bool     `json:"should_warn"`
}

// Flagger evaluates completed LLM responses for hallucination and grounding risk
// using pre-computed scorer outputs passed in by the caller.
type Flagger struct {
	log *zap.Logger
}

// New constructs a Flagger.
func New(logger *zap.Logger) *Flagger {
	return &Flagger{log: logger}
}

// Evaluate computes a RiskFlag from pre-computed scorer outputs. It never
// returns an error; the caller is responsible for ensuring scores are valid
// (zero values produce conservative risk assessments).
func (f *Flagger) Evaluate(
	ctx                     context.Context,
	requestID               string,
	sessionID               string,
	hallucinationScore      float64,
	factualConsistencyScore float64,
	claimDensityRisk        float64,
	flags                   []string,
	ragVerdict              string,
) RiskFlag {
	base := RiskFlag{
		RequestID:         requestID,
		SessionID:         sessionID,
		HallucinationRisk: hallucinationScore,
		GroundingScore:    factualConsistencyScore,
	}

	riskLevel := f.computeRiskLevel(hallucinationScore, factualConsistencyScore)
	base.RiskLevel = riskLevel
	base.ShouldWarn = riskLevel == "high" || riskLevel == "medium"
	base.Reasons = f.buildReasons(hallucinationScore, factualConsistencyScore, claimDensityRisk, riskLevel, flags)
	return f.applyRAGVerdict(base, ragVerdict)
}

func (f *Flagger) applyRAGVerdict(flag RiskFlag, ragVerdict string) RiskFlag {
	switch ragVerdict {
	case "contradicted":
		flag.RiskLevel = "high"
		flag.ShouldWarn = true
		flag.Reasons = append(flag.Reasons, "Response contradicts the provided source document")
	case "unsupported":
		if flag.RiskLevel != "high" {
			flag.RiskLevel = "medium"
			flag.ShouldWarn = true
		}
		flag.Reasons = append(flag.Reasons, "Response contains claims not found in source document")
	}
	return flag
}

func (f *Flagger) computeRiskLevel(hallucination, grounding float64) string {
	if hallucination > 0.7 || grounding < 0.3 {
		return "high"
	}
	if hallucination >= 0.4 || grounding <= 0.5 {
		return "medium"
	}
	return "low"
}

func (f *Flagger) buildReasons(hallucination, grounding, claimDensityRisk float64, riskLevel string, flags []string) []string {
	var reasons []string
	if grounding <= 0.5 {
		reasons = append(reasons, fmt.Sprintf("Response shows weak grounding in the prompt (score: %.2f)", grounding))
	}
	if hallucination >= 0.4 {
		reasons = append(reasons, fmt.Sprintf("High hallucination signal detected (score: %.2f)", hallucination))
	}
	if contains(flags, "high_claim_density") {
		reasons = append(reasons, fmt.Sprintf(
			"High claim density detected — response contains many specific claims on low-context prompt (risk: %.2f)",
			claimDensityRisk,
		))
	}
	if contains(flags, "toxicity_detected") {
		reasons = append(reasons, "Toxic or harmful content detected in response")
	}
	if riskLevel == "low" {
		reasons = append(reasons, "Response is well-grounded and consistent")
	}
	return reasons
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
