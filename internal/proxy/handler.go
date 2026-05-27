package proxy

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ajah/core/internal/config"
	"github.com/ajah/core/internal/events"
	"go.uber.org/zap"
)

var (
	errAzureNotConfigured = errors.New("Azure OpenAI requires endpoint configuration. Set AZURE_OPENAI_ENDPOINT in your environment.")
	supportedProviders    = []string{"openai", "anthropic", "groq", "gemini", "grok", "mistral", "together", "nvidia", "cohere"}
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
			"groq":      "https://api.groq.com/openai/v1",
			"gemini":    "https://generativelanguage.googleapis.com/v1beta/openai",
			"grok":      "https://api.x.ai/v1",
			"mistral":   "https://api.mistral.ai/v1",
			"together":  "https://api.together.xyz/v1",
			"nvidia":    "https://integrate.api.nvidia.com/v1",
			"cohere":    "https://api.cohere.ai/v1",
		},
	}
}

// SetProviderURL overrides the base URL for a named provider.
// Intended for integration tests that need to point the handler at a mock server.
func (h *Handler) SetProviderURL(provider, url string) {
	h.providerURLs[provider] = url
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
		if errors.Is(err, errAzureNotConfigured) {
			log.Warn("azure not configured", zap.Error(err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Warn("unknown provider", zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":               "unrecognized API key format",
			"supported_providers": supportedProviders,
		})
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
		Prompt:       extractPrompt(body),
		Response:     extractResponse(respBody),
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
// Both bare keys ("sk-...") and Bearer-prefixed keys ("Bearer sk-...") are accepted.
func (h *Handler) detectProvider(authHeader string) (provider, url string, err error) {
	key := strings.TrimPrefix(authHeader, "Bearer ")
	switch {
	case strings.HasPrefix(key, "sk-ant-"):
		return "anthropic", h.providerURLs["anthropic"], nil
	case strings.HasPrefix(key, "sk-"):
		return "openai", h.providerURLs["openai"], nil
	case strings.HasPrefix(key, "gsk_"):
		return "groq", h.providerURLs["groq"], nil
	case strings.HasPrefix(key, "AIza"):
		return "gemini", h.providerURLs["gemini"], nil
	case strings.HasPrefix(key, "xai-"):
		return "grok", h.providerURLs["grok"], nil
	case strings.HasPrefix(key, "mistral-"):
		return "mistral", h.providerURLs["mistral"], nil
	case strings.HasPrefix(key, "together-"):
		return "together", h.providerURLs["together"], nil
	case strings.HasPrefix(key, "nvapi-"):
		return "nvidia", h.providerURLs["nvidia"], nil
	case strings.HasPrefix(key, "cohere-"):
		return "cohere", h.providerURLs["cohere"], nil
	case strings.HasPrefix(key, "azure-"):
		return "", "", errAzureNotConfigured
	default:
		return "", "", fmt.Errorf("unrecognized API key format")
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

func extractResponse(body []byte) string {
	// OpenAI format: choices[0].message.content
	var openai struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(body, &openai) == nil && len(openai.Choices) > 0 {
		return openai.Choices[0].Message.Content
	}
	// Anthropic format: content[].text
	var anthropic struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(body, &anthropic) == nil {
		for _, c := range anthropic.Content {
			if c.Type == "text" && c.Text != "" {
				return c.Text
			}
		}
	}
	return ""
}

func extractPrompt(body []byte) string {
	var req struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.Messages) == 0 {
		return ""
	}
	parts := make([]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Content != "" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, " ")
}

func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
