package crossmodel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestVerify_HighAgreement(t *testing.T) {
	scorer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"agreement_score": 0.92,
			"disagreements":   []string{},
		})
	}))
	defer scorer.Close()

	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "Paris is the capital of France."}},
			},
		})
	}))
	defer secondary.Close()

	v := New(scorer.URL, zap.NewNop())
	result, err := v.Verify(
		context.Background(),
		"What is the capital of France?",
		"The capital of France is Paris.",
		"gpt-4o",
		secondary.URL,
		"sk-test",
		"claude-3-5-sonnet",
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != "agree" {
		t.Errorf("Verdict = %q, want agree", result.Verdict)
	}
	if result.RiskLevel != "low" {
		t.Errorf("RiskLevel = %q, want low", result.RiskLevel)
	}
	if result.AgreementScore != 0.92 {
		t.Errorf("AgreementScore = %.4f, want 0.92", result.AgreementScore)
	}
	if result.PrimaryModel != "gpt-4o" {
		t.Errorf("PrimaryModel = %q, want gpt-4o", result.PrimaryModel)
	}
	if result.SecondaryModel != "claude-3-5-sonnet" {
		t.Errorf("SecondaryModel = %q, want claude-3-5-sonnet", result.SecondaryModel)
	}
	if result.SecondaryResponse != "Paris is the capital of France." {
		t.Errorf("SecondaryResponse = %q, want set", result.SecondaryResponse)
	}
}

func TestVerify_Disagreement(t *testing.T) {
	scorer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"agreement_score": 0.30,
			"disagreements": []string{
				"Primary response recommends product A",
				"Secondary response recommends product B",
			},
		})
	}))
	defer scorer.Close()

	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "You should use product B for this task."}},
			},
		})
	}))
	defer secondary.Close()

	v := New(scorer.URL, zap.NewNop())
	result, err := v.Verify(
		context.Background(),
		"Which product should I use?",
		"You should use product A for this task.",
		"gpt-4o",
		secondary.URL,
		"sk-test",
		"claude-3-5-sonnet",
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != "disagree" {
		t.Errorf("Verdict = %q, want disagree", result.Verdict)
	}
	if result.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want high", result.RiskLevel)
	}
	if result.AgreementScore != 0.30 {
		t.Errorf("AgreementScore = %.4f, want 0.30", result.AgreementScore)
	}
	if len(result.Disagreements) != 2 {
		t.Errorf("len(Disagreements) = %d, want 2: %v", len(result.Disagreements), result.Disagreements)
	}
}

func TestVerify_SecondaryProviderFails(t *testing.T) {
	scorer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Scorer must not be reached when the secondary call fails first.
		t.Error("scorer should not be called when secondary provider fails")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer scorer.Close()

	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer secondary.Close()

	v := New(scorer.URL, zap.NewNop())
	result, err := v.Verify(
		context.Background(),
		"What is the answer?",
		"The answer is 42.",
		"gpt-4o",
		secondary.URL,
		"sk-test",
		"claude-3-5-sonnet",
	)

	if err == nil {
		t.Error("expected error when secondary provider returns 500, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result when secondary fails, got %+v", result)
	}
}

func TestVerify_ScorerUnavailable(t *testing.T) {
	// Use identical responses so the Levenshtein ratio is deterministic (1.0 → "agree").
	const sharedResponse = "The sky is blue."

	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": sharedResponse}},
			},
		})
	}))
	defer secondary.Close()

	// Dead scorer address — connection refused triggers Levenshtein fallback.
	v := New("http://127.0.0.1:19998", zap.NewNop())
	result, err := v.Verify(
		context.Background(),
		"What color is the sky?",
		sharedResponse, // same as secondary → ratio = 1.0
		"gpt-4o",
		secondary.URL,
		"sk-test",
		"claude-3-5-sonnet",
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result when scorer is unavailable")
	}
	// Identical strings → levenshteinRatio = 1.0 → "agree", "low"
	if result.Verdict != "agree" {
		t.Errorf("Verdict = %q, want agree (Levenshtein fallback, identical responses)", result.Verdict)
	}
	if result.RiskLevel != "low" {
		t.Errorf("RiskLevel = %q, want low", result.RiskLevel)
	}
	if result.AgreementScore < 0.99 {
		t.Errorf("AgreementScore = %.4f, want ~1.0 (Levenshtein of identical strings)", result.AgreementScore)
	}
}
