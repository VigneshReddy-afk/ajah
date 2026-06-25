package proxy

import (
	"encoding/json"
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
	return New(cfg, em, zap.NewNop(), nil, nil, nil)
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
	req.Header.Set("X-Parent-Step-ID", "parent-step-0")
	req.Header.Set("X-Tool-Name", "web_search")

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
	if evt.StepName != "step-1" {
		t.Errorf("StepName = %q, want step-1", evt.StepName)
	}
	if evt.ParentStepID != "parent-step-0" {
		t.Errorf("ParentStepID = %q, want parent-step-0", evt.ParentStepID)
	}
	if evt.ToolName != "web_search" {
		t.Errorf("ToolName = %q, want web_search", evt.ToolName)
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

func TestBuildTargetURL(t *testing.T) {
	cases := []struct {
		providerURL string
		requestURI  string
		want        string
	}{
		// Provider base has no path → full requestURI appended as-is
		{"https://api.openai.com", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://api.anthropic.com", "/v1/chat/completions", "https://api.anthropic.com/v1/chat/completions"},
		// Provider base already includes a version path → strip /v1 from requestURI
		{"https://api.groq.com/openai/v1", "/v1/chat/completions", "https://api.groq.com/openai/v1/chat/completions"},
		{"https://api.x.ai/v1", "/v1/chat/completions", "https://api.x.ai/v1/chat/completions"},
		{"https://api.mistral.ai/v1", "/v1/chat/completions", "https://api.mistral.ai/v1/chat/completions"},
		{"https://api.together.xyz/v1", "/v1/chat/completions", "https://api.together.xyz/v1/chat/completions"},
		{"https://integrate.api.nvidia.com/v1", "/v1/chat/completions", "https://integrate.api.nvidia.com/v1/chat/completions"},
		{"https://api.cohere.ai/v1", "/v1/chat/completions", "https://api.cohere.ai/v1/chat/completions"},
		{"https://generativelanguage.googleapis.com/v1beta/openai", "/v1/chat/completions", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"},
	}
	for _, tc := range cases {
		got := buildTargetURL(tc.providerURL, tc.requestURI)
		if got != tc.want {
			t.Errorf("buildTargetURL(%q, %q)\n  got  %q\n  want %q", tc.providerURL, tc.requestURI, got, tc.want)
		}
	}
}

func TestHandler_RoutesAdditionalProviders(t *testing.T) {
	cases := []struct {
		name     string
		authKey  string
		provider string
	}{
		{"groq", "gsk_testkey", "groq"},
		{"gemini", "AIzatestkey", "gemini"},
		{"grok", "xai-testkey", "grok"},
		{"mistral", "mistral-testkey", "mistral"},
		{"together", "together-testkey", "together"},
		{"nvidia", "nvapi-testkey", "nvidia"},
		{"cohere", "cohere-testkey", "cohere"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := mockProvider(t, http.StatusOK, llmResponse)
			defer upstream.Close()

			em := newMockEmitter()
			h := newHandler(t, testConfig(), em)
			h.providerURLs[tc.provider] = upstream.URL

			srv := httptest.NewServer(h)
			defer srv.Close()

			resp := doPost(t, srv.URL+"/v1/chat/completions", tc.authKey, `{"model":"test-model","messages":[]}`)
			resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			evt := mustEvent(t, em)
			if evt.Provider != tc.provider {
				t.Errorf("Provider = %q, want %q", evt.Provider, tc.provider)
			}
		})
	}
}

func TestHandler_UpstreamAlwaysReceivesBearerPrefix(t *testing.T) {
	cases := []struct {
		name     string
		authSent string
		wantAuth string
	}{
		{"bare openai key gets Bearer", "sk-test-key", "Bearer sk-test-key"},
		{"Bearer openai key preserved", "Bearer sk-test-key", "Bearer sk-test-key"},
		{"bare groq key gets Bearer", "gsk_testkey", "Bearer gsk_testkey"},
		{"Bearer groq key preserved", "Bearer gsk_testkey", "Bearer gsk_testkey"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotAuth string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(llmResponse))
			}))
			defer upstream.Close()

			em := newMockEmitter()
			h := newHandler(t, testConfig(), em)
			h.providerURLs["openai"] = upstream.URL
			h.providerURLs["groq"] = upstream.URL

			srv := httptest.NewServer(h)
			defer srv.Close()

			resp := doPost(t, srv.URL+"/v1/chat/completions", tc.authSent, `{"model":"gpt-4","messages":[]}`)
			resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			if gotAuth != tc.wantAuth {
				t.Errorf("upstream Authorization = %q, want %q", gotAuth, tc.wantAuth)
			}
			em.next(time.Second)
		})
	}
}

func TestHandler_RejectsAzureKeyWithPlainTextError(t *testing.T) {
	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "azure-mykey", `{"model":"gpt-4","messages":[]}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Azure OpenAI") {
		t.Errorf("body = %q, want Azure error message", body)
	}
	var v map[string]interface{}
	if json.Unmarshal(body, &v) == nil {
		t.Errorf("expected plain text response, got JSON: %s", body)
	}
}

func TestHandler_RejectsUnknownKeyWithJSONError(t *testing.T) {
	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "Bearer unknown-xyz-key", `{"model":"x","messages":[]}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	body, _ := io.ReadAll(resp.Body)
	var v map[string]interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("response is not JSON: %v\nbody: %s", err, body)
	}
	if v["error"] != "unrecognized API key format" {
		t.Errorf("error = %q, want 'unrecognized API key format'", v["error"])
	}
	if _, ok := v["supported_providers"]; !ok {
		t.Errorf("response missing 'supported_providers' key")
	}
}

func TestHandler_AjahRequestIDHeaderPresent(t *testing.T) {
	provider := mockProvider(t, http.StatusOK, llmResponse)
	defer provider.Close()

	em := newMockEmitter()
	h := newHandler(t, testConfig(), em)
	h.providerURLs["openai"] = provider.URL

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/v1/chat/completions", "sk-test-key", `{"model":"gpt-4","messages":[]}`)
	resp.Body.Close()

	got := resp.Header.Get("X-Ajah-Request-ID")
	if got == "" {
		t.Error("X-Ajah-Request-ID header is missing")
	}
	em.next(time.Second)
}

func TestHandler_HonorsXRequestIDHeader(t *testing.T) {
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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "my-custom-id")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	resp.Body.Close()

	got := resp.Header.Get("X-Ajah-Request-ID")
	if got != "my-custom-id" {
		t.Errorf("X-Ajah-Request-ID = %q, want %q", got, "my-custom-id")
	}
	em.next(time.Second)
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
