package proxy

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ajah/core/internal/config"
	"github.com/ajah/core/internal/events"
	"go.uber.org/zap"
)

// eventSink is the interface the Handler depends on for async event dispatch.
// Keeping it local and unexported means any type with a matching Emit method
// satisfies it — including *events.Emitter in production and mockEmitter in tests.
type eventSink interface {
	Emit(event events.RequestEvent)
}

// Handler is the HTTP reverse proxy for LLM provider APIs.
type Handler struct {
	cfg          *config.Config
	emitter      eventSink
	logger       *zap.Logger
	client       *http.Client
	providerURLs map[string]string
}

// New creates a Handler with the given configuration, emitter, and logger.
func New(cfg *config.Config, em eventSink, logger *zap.Logger) *Handler {
	return &Handler{
		cfg:     cfg,
		emitter: em,
		logger:  logger,
		client: &http.Client{
			Timeout: time.Duration(cfg.ProviderTimeoutSeconds) * time.Second,
		},
		providerURLs: map[string]string{
			"openai":    "https://api.openai.com",
			"anthropic": "https://api.anthropic.com",
		},
	}
}

// ServeHTTP proxies the request to the appropriate LLM provider.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := newRequestID()
	log := h.logger.With(zap.String("request_id", requestID))

	if r.Method != http.MethodPost {
		log.Warn("method not allowed", zap.String("method", r.Method))
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("X-User-ID")
	sessionID := r.Header.Get("X-Session-ID")
	featureName := r.Header.Get("X-Feature-Name")
	agentStep := r.Header.Get("X-Agent-Step")

	provider, providerURL, err := h.detectProvider(r.Header.Get("Authorization"))
	if err != nil {
		log.Warn("unknown provider", zap.Error(err))
		http.Error(w, fmt.Sprintf("unknown provider: %s", err), http.StatusBadRequest)
		return
	}

	// Read up to maxBytes+1 so we can detect bodies that exceed the limit.
	body, err := io.ReadAll(io.LimitReader(r.Body, h.cfg.MaxRequestBodyBytes+1))
	if err != nil {
		log.Error("failed to read request body", zap.Error(err))
		http.Error(w, "failed to read request body", http.StatusInternalServerError)
		return
	}
	_ = r.Body.Close()

	if int64(len(body)) > h.cfg.MaxRequestBodyBytes {
		log.Warn("request body too large",
			zap.Int("size", len(body)),
			zap.Int64("limit", h.cfg.MaxRequestBodyBytes),
		)
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	model := extractModel(body)

	targetURL := providerURL + r.URL.RequestURI()
	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		log.Error("failed to build upstream request", zap.Error(err))
		http.Error(w, "failed to build upstream request", http.StatusInternalServerError)
		return
	}
	copyHeaders(upstreamReq.Header, r.Header)

	log.Info("forwarding request",
		zap.String("provider", provider),
		zap.String("target_url", targetURL),
		zap.String("model", model),
		zap.String("user_id", userID),
	)

	resp, err := h.client.Do(upstreamReq)
	if err != nil {
		if r.Context().Err() != nil {
			log.Info("request cancelled by client", zap.Error(r.Context().Err()))
			return
		}
		log.Error("upstream request failed", zap.Error(err))
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Buffer the full response body so we can parse tokens before writing it out.
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("failed to read upstream response body", zap.Error(err))
		http.Error(w, "failed to read upstream response", http.StatusBadGateway)
		return
	}

	inputTokens, outputTokens := extractTokens(respBody)
	latencyMs := time.Since(start).Milliseconds()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(respBody); err != nil {
		log.Warn("failed to write response body to client", zap.Error(err))
	}

	event := events.RequestEvent{
		RequestID:    requestID,
		UserID:       userID,
		SessionID:    sessionID,
		FeatureName:  featureName,
		AgentStep:    agentStep,
		Provider:     provider,
		Model:        model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		LatencyMs:    latencyMs,
		StatusCode:   resp.StatusCode,
		Timestamp:    start,
	}

	// Fire-and-forget: never block the caller on event processing.
	go h.emitter.Emit(event)

	log.Info("request completed",
		zap.String("provider", provider),
		zap.String("user_id", userID),
		zap.String("model", model),
		zap.Int64("latency_ms", latencyMs),
		zap.Int("status_code", resp.StatusCode),
		zap.Int("input_tokens", inputTokens),
		zap.Int("output_tokens", outputTokens),
	)
}

// detectProvider maps the Authorization header prefix to a provider name and base URL.
func (h *Handler) detectProvider(authHeader string) (provider, url string, err error) {
	switch {
	case strings.HasPrefix(authHeader, "sk-ant-"):
		return "anthropic", h.providerURLs["anthropic"], nil
	case strings.HasPrefix(authHeader, "sk-"):
		return "openai", h.providerURLs["openai"], nil
	default:
		return "", "", fmt.Errorf(
			"authorization header prefix not recognized: expected 'sk-ant-' (Anthropic) or 'sk-' (OpenAI)",
		)
	}
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}

type providerResponse struct {
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func extractTokens(body []byte) (inputTokens, outputTokens int) {
	var r providerResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return 0, 0
	}
	return r.Usage.PromptTokens, r.Usage.CompletionTokens
}

func extractModel(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.Model
}

func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
