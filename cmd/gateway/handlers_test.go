package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ajah/core/internal/flagging"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, rdb
}

func TestWarningByRequestID_ReturnsFlagFromRedis(t *testing.T) {
	mr, rdb := newTestRedis(t)
	defer mr.Close()

	fr := flagResult{
		RequestID: "test-req-001",
		Flagged:   true,
		RiskLevel: "high",
		Reasons:   []string{"High hallucination signal detected (score: 0.80)"},
	}
	data, _ := json.Marshal(fr)
	mr.Set("flag:test-req-001", string(data))

	router := chi.NewRouter()
	router.Get("/warnings/{requestID}", warningByRequestIDHandler(rdb, zap.NewNop()))

	req := httptest.NewRequest(http.MethodGet, "/warnings/test-req-001", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var got flagResult
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.RequestID != "test-req-001" {
		t.Errorf("RequestID = %q, want test-req-001", got.RequestID)
	}
	if !got.Flagged {
		t.Error("Flagged = false, want true")
	}
	if got.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want high", got.RiskLevel)
	}
}

func TestWarningByRequestID_Returns404WhenNotFound(t *testing.T) {
	_, rdb := newTestRedis(t)

	router := chi.NewRouter()
	router.Get("/warnings/{requestID}", warningByRequestIDHandler(rdb, zap.NewNop()))

	req := httptest.NewRequest(http.MethodGet, "/warnings/no-such-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestFireWebhook_PostsToWebhookURL(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	flag := flagging.RiskFlag{
		RequestID:         "req-webhook-001",
		SessionID:         "sess-001",
		HallucinationRisk: 0.85,
		GroundingScore:    0.20,
		RiskLevel:         "high",
		Reasons:           []string{"High hallucination signal detected (score: 0.85)"},
		ShouldWarn:        true,
	}

	client := &http.Client{Timeout: 5 * time.Second}
	fireWebhook(srv.URL, flag, client, zap.NewNop())

	if received == nil {
		t.Fatal("webhook server received no request")
	}

	var got flagging.RiskFlag
	if err := json.Unmarshal(received, &got); err != nil {
		t.Fatalf("decode webhook payload: %v", err)
	}
	if got.RequestID != "req-webhook-001" {
		t.Errorf("RequestID = %q, want req-webhook-001", got.RequestID)
	}
	if got.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want high", got.RiskLevel)
	}
	if !got.ShouldWarn {
		t.Error("ShouldWarn = false, want true")
	}
}

func TestFireWebhook_RetriesOnFailure(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		_, _ = io.ReadAll(r.Body)
		if attempts == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	flag := flagging.RiskFlag{
		RequestID: "req-retry-001",
		RiskLevel: "high",
		ShouldWarn: true,
	}

	client := &http.Client{Timeout: 5 * time.Second}
	fireWebhook(srv.URL, flag, client, zap.NewNop())

	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

