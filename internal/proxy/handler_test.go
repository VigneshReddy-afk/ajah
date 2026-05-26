package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ajah/core/internal/config"
	"github.com/ajah/core/internal/events"
	"go.uber.org/zap"
)

// mockEmitter captures emitted events on a typed buffered channel.
type mockEmitter struct {
	ch chan events.RequestEvent
}

func newMockEmitter() *mockEmitter {
	return &mockEmitter{ch: make(chan events.RequestEvent, 16)}
}

func (m *mockEmitter) Emit(event events.RequestEvent) {
	m.ch <- event
}

// next blocks until an event arrives or the timeout elapses.
func (m *mockEmitter) next(timeout time.Duration) (events.RequestEvent, bool) {
	select {
	case e := <-m.ch:
		return e, true
	case <-time.After(timeout):
		return events.RequestEvent{}, false
	}
}

// Compile-time check: mockEmitter satisfies the local eventSink interface.
var _ eventSink = (*mockEmitter)(nil)

// ---- helpers ----------------------------------------------------------------

func testConfig() *config.Config {
	return &config.Config{
		Port:                   "8080",
		RedisURL:               "redis://localhost:6379",
		DatabaseURL:            "postgres://localhost:5432/observatory",
		LogLevel:               "info",
		MaxRequestBodyBytes:    10 * 1024 * 1024,
		AsyncWorkerCount:       10,
		ProviderTimeoutSeconds: 30,
	}
}

// newHandler builds a Handler with a no-op logger to keep test output clean.
func newHandler(t *testing.T, cfg *config.Config, em *mockEmitter) *Handler {
	t.Helper()
	return New(cfg, em, zap.NewNop())
}

const llmResponse = `{"id":"chatcmpl-1","model":"gpt-4","usage":{"prompt_tokens":15,"completion_tokens":25}}`

// mockProvider starts an httptest server that always returns statusCode and body.
func mockProvider(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
}

// doPost fires a POST to url with the given Authorization header and JSON body.
func doPost(t *testing.T, url, authKey, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Authorization", authKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("executing request: %v", err)
	}
	return resp
}

// mustEvent waits up to one second for an event and fails the test on timeout.
func mustEvent(t *testing.T, em *mockEmitter) events.RequestEvent {
	t.Helper()
	evt, ok := em.next(time.Second)
	if !ok {
		t.Fatal("timed out waiting for emitted event")
	}
	return evt
}

// ---- tests ------------------------------------------------------------------

func TestHandler_RoutesOpenAIProvider(t *testing.T) {
	provider := mockProvider(t, http.StatusOK, llmResponse)
	defer provider.Close()

	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	h.providerURLs["openai"] = provider.URL

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "sk-test-key", `{"model":"gpt-4","messages":[]}`)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	evt := mustEvent(t, em)
	if evt.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", evt.Provider)
	}
}

func TestHandler_RoutesAnthropicProvider(t *testing.T) {
	provider := mockProvider(t, http.StatusOK, llmResponse)
	defer provider.Close()

	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	h.providerURLs["anthropic"] = provider.URL

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "sk-ant-test-key", `{"model":"claude-3","messages":[]}`)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	evt := mustEvent(t, em)
	if evt.Provider != "anthropic" {
		t.Errorf("Provider = %q, want anthropic", evt.Provider)
	}
}

func TestHandler_RejectsUnknownProvider(t *testing.T) {
	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "Bearer unknown-key", `{"model":"x","messages":[]}`)
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandler_RejectsNonPostMethod(t *testing.T) {
	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/chat/completions", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHandler_ExtractsMetadataHeaders(t *testing.T) {
	provider := mockProvider(t, http.StatusOK, llmResponse)
	defer provider.Close()

	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	h.providerURLs["openai"] = provider.URL

	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4","messages":[]}`))
	req.Header.Set("Authorization", "sk-test-key")
	req.Header.Set("X-User-ID", "user-42")
	req.Header.Set("X-Session-ID", "sess-99")
	req.Header.Set("X-Feature-Name", "chat-feature")
	req.Header.Set("X-Agent-Step", "step-1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	resp.Body.Close()

	evt := mustEvent(t, em)
	if evt.UserID != "user-42" {
		t.Errorf("UserID = %q, want user-42", evt.UserID)
	}
	if evt.SessionID != "sess-99" {
		t.Errorf("SessionID = %q, want sess-99", evt.SessionID)
	}
	if evt.FeatureName != "chat-feature" {
		t.Errorf("FeatureName = %q, want chat-feature", evt.FeatureName)
	}
	if evt.AgentStep != "step-1" {
		t.Errorf("AgentStep = %q, want step-1", evt.AgentStep)
	}
}

func TestHandler_ParsesTokenUsageFromResponse(t *testing.T) {
	provider := mockProvider(t, http.StatusOK, llmResponse)
	defer provider.Close()

	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	h.providerURLs["openai"] = provider.URL

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "sk-test-key", `{"model":"gpt-4","messages":[]}`)
	resp.Body.Close()

	evt := mustEvent(t, em)
	if evt.InputTokens != 15 {
		t.Errorf("InputTokens = %d, want 15", evt.InputTokens)
	}
	if evt.OutputTokens != 25 {
		t.Errorf("OutputTokens = %d, want 25", evt.OutputTokens)
	}
}

func TestHandler_ForwardsProviderStatusCode(t *testing.T) {
	errBody := `{"error":{"message":"model not found","type":"invalid_request_error"}}`
	provider := mockProvider(t, http.StatusNotFound, errBody)
	defer provider.Close()

	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	h.providerURLs["openai"] = provider.URL

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "sk-test-key", `{"model":"gpt-99","messages":[]}`)
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestHandler_RejectsOversizeBody(t *testing.T) {
	cfg := testConfig()
	cfg.MaxRequestBodyBytes = 10

	em := newMockEmitter()
	h := newHandler(t, cfg, em)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "sk-test-key", strings.Repeat("x", 100))
	resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
}

func TestHandler_ResponseBodyPassedThroughUnchanged(t *testing.T) {
	provider := mockProvider(t, http.StatusOK, llmResponse)
	defer provider.Close()

	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	h.providerURLs["openai"] = provider.URL

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "sk-test-key", `{"model":"gpt-4","messages":[]}`)
	defer resp.Body.Close()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(got) != llmResponse {
		t.Errorf("body = %s\nwant %s", got, llmResponse)
	}
}

func TestHandler_BearerPrefixRoutesOpenAI(t *testing.T) {
	provider := mockProvider(t, http.StatusOK, llmResponse)
	defer provider.Close()

	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	h.providerURLs["openai"] = provider.URL

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "Bearer sk-test123", `{"model":"gpt-4","messages":[]}`)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	evt := mustEvent(t, em)
	if evt.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", evt.Provider)
	}
}

func TestHandler_BearerPrefixRoutesAnthropic(t *testing.T) {
	provider := mockProvider(t, http.StatusOK, llmResponse)
	defer provider.Close()

	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	h.providerURLs["anthropic"] = provider.URL

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "Bearer sk-ant-test123", `{"model":"claude-3","messages":[]}`)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	evt := mustEvent(t, em)
	if evt.Provider != "anthropic" {
		t.Errorf("Provider = %q, want anthropic", evt.Provider)
	}
}

func TestHandler_EventTimestampAndLatency(t *testing.T) {
	provider := mockProvider(t, http.StatusOK, llmResponse)
	defer provider.Close()

	before := time.Now()

	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	h.providerURLs["openai"] = provider.URL

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "sk-test-key", `{"model":"gpt-4","messages":[]}`)
	resp.Body.Close()

	after := time.Now()

	evt := mustEvent(t, em)
	if evt.Timestamp.Before(before) {
		t.Errorf("Timestamp %v is before test start %v", evt.Timestamp, before)
	}
	if evt.Timestamp.After(after) {
		t.Errorf("Timestamp %v is after test end %v", evt.Timestamp, after)
	}
	if evt.LatencyMs < 0 {
		t.Errorf("LatencyMs = %d, must be >= 0", evt.LatencyMs)
	}
}
