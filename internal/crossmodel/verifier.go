package crossmodel

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

// VerificationResult holds the outcome of a cross-model consistency check.
type VerificationResult struct {
	PrimaryModel      string
	SecondaryModel    string
	PrimaryResponse   string
	SecondaryResponse string
	AgreementScore    float64  // 0.0 to 1.0
	Disagreements     []string // specific points of disagreement
	Verdict           string   // "agree", "partial", or "disagree"
	RiskLevel         string   // "low", "medium", or "high"
}

// Verifier sends a prompt to a secondary LLM provider and compares the
// response to the primary response using the quality scorer service.
type Verifier struct {
	httpClient *http.Client
	scorerURL  string
	logger     *zap.Logger
}

// New creates a Verifier that calls scorerURL for semantic comparison.
func New(scorerURL string, logger *zap.Logger) *Verifier {
	return &Verifier{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		scorerURL:  scorerURL,
		logger:     logger,
	}
}

type chatReq struct {
	Model    string    `json:"model"`
	Messages []chatMsg `json:"messages"`
}

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type compareReq struct {
	ResponseA string `json:"response_a"`
	ResponseB string `json:"response_b"`
	Prompt    string `json:"prompt"`
}

type compareResp struct {
	AgreementScore float64  `json:"agreement_score"`
	Disagreements  []string `json:"disagreements"`
}

// Verify sends prompt to secondaryProviderURL, compares the result against
// primaryResponse via the scorer, and returns a VerificationResult.
// Returns an error only if the secondary provider call fails; scorer failures
// fall back to Levenshtein similarity and never return an error.
func (v *Verifier) Verify(
	ctx context.Context,
	prompt, primaryResponse, primaryModel,
	secondaryProviderURL, secondaryAPIKey, secondaryModel string,
) (*VerificationResult, error) {
	// Step 1: send the same prompt to the secondary provider.
	secondaryResponse, err := v.callSecondary(ctx, prompt, secondaryProviderURL, secondaryAPIKey, secondaryModel)
	if err != nil {
		return nil, fmt.Errorf("crossmodel: secondary call failed: %w", err)
	}

	// Step 2: score agreement via scorer (Levenshtein fallback if unavailable).
	score, disagreements := v.compareResponses(ctx, prompt, primaryResponse, secondaryResponse)

	// Step 3: classify by agreement threshold.
	verdict, riskLevel := classify(score)

	return &VerificationResult{
		PrimaryModel:      primaryModel,
		SecondaryModel:    secondaryModel,
		PrimaryResponse:   primaryResponse,
		SecondaryResponse: secondaryResponse,
		AgreementScore:    score,
		Disagreements:     disagreements,
		Verdict:           verdict,
		RiskLevel:         riskLevel,
	}, nil
}

func (v *Verifier) callSecondary(ctx context.Context, prompt, providerURL, apiKey, model string) (string, error) {
	body, err := json.Marshal(chatReq{
		Model:    model,
		Messages: []chatMsg{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, providerURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("secondary returned %d: %s", resp.StatusCode, string(raw))
	}

	var r chatResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(r.Choices) == 0 {
		return "", fmt.Errorf("secondary returned empty choices")
	}
	return r.Choices[0].Message.Content, nil
}

func (v *Verifier) compareResponses(ctx context.Context, prompt, a, b string) (float64, []string) {
	body, err := json.Marshal(compareReq{ResponseA: a, ResponseB: b, Prompt: prompt})
	if err != nil {
		v.logger.Warn("crossmodel: marshal compare payload failed", zap.Error(err))
		return levenshteinRatio(a, b), []string{}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.scorerURL+"/compare", bytes.NewReader(body))
	if err != nil {
		v.logger.Warn("crossmodel: build compare request failed", zap.Error(err))
		return levenshteinRatio(a, b), []string{}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		v.logger.Warn("crossmodel: scorer unreachable, using Levenshtein fallback",
			zap.String("url", v.scorerURL), zap.Error(err))
		return levenshteinRatio(a, b), []string{}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		v.logger.Warn("crossmodel: read scorer response failed", zap.Error(err))
		return levenshteinRatio(a, b), []string{}
	}

	var r compareResp
	if err := json.Unmarshal(raw, &r); err != nil {
		v.logger.Warn("crossmodel: decode scorer response failed",
			zap.String("body", string(raw)), zap.Error(err))
		return levenshteinRatio(a, b), []string{}
	}

	if r.Disagreements == nil {
		r.Disagreements = []string{}
	}
	return r.AgreementScore, r.Disagreements
}

func classify(score float64) (verdict, riskLevel string) {
	switch {
	case score >= 0.80:
		return "agree", "low"
	case score >= 0.50:
		return "partial", "medium"
	default:
		return "disagree", "high"
	}
}

// levenshteinRatio returns a similarity ratio in [0,1] between a and b.
// Inputs are capped at 1000 runes to bound memory for very long responses.
func levenshteinRatio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) > 1000 {
		ra = ra[:1000]
	}
	if len(rb) > 1000 {
		rb = rb[:1000]
	}
	la, lb := len(ra), len(rb)
	if la == 0 && lb == 0 {
		return 1.0
	}
	if la == 0 || lb == 0 {
		return 0.0
	}
	dist := levenshteinDist(ra, rb)
	maxLen := la
	if lb > maxLen {
		maxLen = lb
	}
	return 1.0 - float64(dist)/float64(maxLen)
}

func levenshteinDist(a, b []rune) int {
	la, lb := len(a), len(b)
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1]
			} else {
				curr[j] = 1 + minOf3(prev[j-1], prev[j], curr[j-1])
			}
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func minOf3(a, b, c int) int {
	if a <= b && a <= c {
		return a
	}
	if b <= c {
		return b
	}
	return c
}
