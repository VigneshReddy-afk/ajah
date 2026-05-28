package flagging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// RiskFlag is the evaluated risk output for a single completed LLM response.
type RiskFlag struct {
	RequestID         string   `json:"request_id"`
	SessionID         string   `json:"session_id"`
	HallucinationRisk float64  `json:"hallucination_risk"` // 0.0 to 1.0
	GroundingScore    float64  `json:"grounding_score"`    // 0.0 to 1.0
	RiskLevel         string   `json:"risk_level"`         // "low", "medium", "high", or "unknown"
	Reasons           []string `json:"reasons"`
	ShouldWarn        bool     `json:"should_warn"`
}

// Flagger evaluates completed LLM responses for hallucination and grounding
// risk by calling the quality scorer service. It is designed to run in the
// async pipeline after the response has already been returned to the caller.
type Flagger struct {
	scorerURL string
	client    *http.Client
	log       *zap.Logger
}

// New constructs a Flagger that calls the scorer at scorerURL.
func New(scorerURL string, logger *zap.Logger) *Flagger {
	return &Flagger{
		scorerURL: scorerURL,
		client:    &http.Client{Timeout: 10 * time.Second},
		log:       logger,
	}
}

type scorerPayload struct {
	RequestID   string `json:"request_id"`
	Prompt      string `json:"prompt"`
	Response    string `json:"response"`
	Model       string `json:"model"`
	FeatureName string `json:"feature_name"`
}

type scorerResult struct {
	HallucinationScore      float64 `json:"hallucination_score"`
	FactualConsistencyScore float64 `json:"factual_consistency_score"`
}

// Evaluate scores a completed LLM response and returns a RiskFlag. It never
// returns an error: if the scorer is unreachable the returned flag has
// RiskLevel "unknown" so the async pipeline can always continue.
func (f *Flagger) Evaluate(ctx context.Context, requestID, sessionID, prompt, response string, qualityScore float64) RiskFlag {
	base := RiskFlag{
		RequestID: requestID,
		SessionID: sessionID,
	}

	result, ok := f.callScorer(ctx, requestID, prompt, response)
	if !ok {
		base.RiskLevel = "unknown"
		base.ShouldWarn = false
		base.Reasons = []string{"scoring unavailable"}
		return base
	}

	base.HallucinationRisk = result.HallucinationScore
	base.GroundingScore = result.FactualConsistencyScore

	riskLevel := f.computeRiskLevel(result.HallucinationScore, result.FactualConsistencyScore)
	base.RiskLevel = riskLevel
	base.ShouldWarn = riskLevel == "high" || riskLevel == "medium"
	base.Reasons = f.buildReasons(result.HallucinationScore, result.FactualConsistencyScore, riskLevel)
	return base
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

func (f *Flagger) buildReasons(hallucination, grounding float64, riskLevel string) []string {
	var reasons []string
	if grounding <= 0.5 {
		reasons = append(reasons, fmt.Sprintf("Response shows weak grounding in the prompt (score: %.2f)", grounding))
	}
	if hallucination >= 0.4 {
		reasons = append(reasons, fmt.Sprintf("High hallucination signal detected (score: %.2f)", hallucination))
	}
	if riskLevel == "low" {
		reasons = append(reasons, "Response is well-grounded and consistent")
	}
	return reasons
}

func (f *Flagger) callScorer(ctx context.Context, requestID, prompt, response string) (scorerResult, bool) {
	body, err := json.Marshal(scorerPayload{
		RequestID:   requestID,
		Prompt:      prompt,
		Response:    response,
		Model:       "unknown",
		FeatureName: "flagging",
	})
	if err != nil {
		f.log.Warn("flagger: marshal failed", zap.Error(err))
		return scorerResult{}, false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.scorerURL+"/score", bytes.NewReader(body))
	if err != nil {
		f.log.Warn("flagger: build request failed", zap.Error(err))
		return scorerResult{}, false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		f.log.Warn("flagger: scorer unreachable", zap.String("url", f.scorerURL), zap.Error(err))
		return scorerResult{}, false
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		f.log.Warn("flagger: read response failed", zap.Error(err))
		return scorerResult{}, false
	}

	var result scorerResult
	if err := json.Unmarshal(raw, &result); err != nil {
		f.log.Warn("flagger: decode failed", zap.String("body", string(raw)), zap.Error(err))
		return scorerResult{}, false
	}

	return result, true
}
