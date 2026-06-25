// Package fallback provides automatic provider failover for the Ajah gateway.
// When a primary LLM provider returns a non-retryable error or 5xx response,
// the manager transparently retries the request against a configured backup provider.
//
// Health state is tracked in Redis:
//   - Key: ajah:fallback:failures:{provider}  — sliding counter, TTL 60s
//   - Key: ajah:fallback:degraded:{provider}  — present means provider is in cooldown
//
// Thresholds (configurable via Manager options):
//   - FailureThreshold: 3 failures in 60s → provider marked degraded
//   - CooldownSeconds:  120s before a degraded provider is retried again
package fallback

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	failureWindow    = 60 * time.Second
	cooldownDuration = 2 * time.Minute
	failureThreshold = 3
)

// ProviderConfig holds the credentials and URL for a fallback provider.
type ProviderConfig struct {
	// Model is the model string to send to the fallback provider (e.g. "gpt-4o-mini").
	Model string
	// URL is the base API URL (e.g. "https://api.openai.com/v1").
	URL string
	// APIKey is the Bearer token for the fallback provider.
	APIKey string
}

// Manager decides whether to use a fallback provider and executes the fallback call.
type Manager struct {
	rdb      *redis.Client
	logger   *zap.Logger
	fallback *ProviderConfig
	client   *http.Client
}

// New returns a Manager. If fallback is nil, the manager is a no-op (IsEnabled returns false).
func New(rdb *redis.Client, logger *zap.Logger, fallback *ProviderConfig) *Manager {
	return &Manager{
		rdb:      rdb,
		logger:   logger,
		fallback: fallback,
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

// IsEnabled returns true if a fallback provider is configured.
func (m *Manager) IsEnabled() bool {
	return m.fallback != nil && m.fallback.URL != "" && m.fallback.APIKey != ""
}

// ShouldFallback returns true if the given primary provider is currently degraded.
func (m *Manager) ShouldFallback(ctx context.Context, provider string) bool {
	if !m.IsEnabled() {
		return false
	}
	degradedKey := fmt.Sprintf("ajah:fallback:degraded:%s", provider)
	val, err := m.rdb.Exists(ctx, degradedKey).Result()
	if err != nil {
		return false
	}
	return val > 0
}

// RecordFailure increments the failure counter for a provider.
// If the counter crosses failureThreshold within failureWindow, the provider is marked degraded.
func (m *Manager) RecordFailure(ctx context.Context, provider string) {
	if !m.IsEnabled() || m.rdb == nil {
		return
	}
	failKey := fmt.Sprintf("ajah:fallback:failures:%s", provider)
	pipe := m.rdb.Pipeline()
	pipe.Incr(ctx, failKey)
	pipe.Expire(ctx, failKey, failureWindow)
	results, err := pipe.Exec(ctx)
	if err != nil || len(results) < 1 {
		return
	}
	count, _ := results[0].(*redis.IntCmd).Result()
	if count >= failureThreshold {
		degradedKey := fmt.Sprintf("ajah:fallback:degraded:%s", provider)
		m.rdb.Set(ctx, degradedKey, "1", cooldownDuration)
		m.logger.Warn("provider marked degraded — activating fallback",
			zap.String("provider", provider),
			zap.Int64("failure_count", count),
			zap.Duration("cooldown", cooldownDuration),
		)
	}
}

// RecordSuccess clears failure counters for a provider (called on successful responses).
func (m *Manager) RecordSuccess(ctx context.Context, provider string) {
	if !m.IsEnabled() || m.rdb == nil {
		return
	}
	failKey := fmt.Sprintf("ajah:fallback:failures:%s", provider)
	m.rdb.Del(ctx, failKey)
}

// IsDegradedStatusCode returns true for HTTP status codes that should trigger fallback.
// 429 (rate limit) and 5xx (server errors) trigger fallback; 4xx client errors do not.
func IsDegradedStatusCode(code int) bool {
	return code == http.StatusTooManyRequests || (code >= 500 && code <= 599)
}

// FallbackResult holds the response from a fallback call.
type FallbackResult struct {
	Body       []byte
	StatusCode int
	UsedModel  string
	UsedURL    string
}

// Execute rewrites the original request body to use the fallback model and
// fires it against the fallback provider URL. Returns the raw response body.
func (m *Manager) Execute(ctx context.Context, originalBody []byte, originalPath string) (*FallbackResult, error) {
	if !m.IsEnabled() {
		return nil, fmt.Errorf("no fallback provider configured")
	}

	// Rewrite the model field in the JSON body to use the fallback model
	rewritten, err := rewriteModel(originalBody, m.fallback.Model)
	if err != nil {
		// If rewrite fails, send the original body unchanged
		rewritten = originalBody
	}

	targetURL := strings.TrimRight(m.fallback.URL, "/") + originalPath

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(rewritten))
	if err != nil {
		return nil, fmt.Errorf("build fallback request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.fallback.APIKey)

	m.logger.Info("executing fallback request",
		zap.String("fallback_url", targetURL),
		zap.String("fallback_model", m.fallback.Model),
	)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fallback request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read fallback response: %w", err)
	}

	return &FallbackResult{
		Body:       body,
		StatusCode: resp.StatusCode,
		UsedModel:  m.fallback.Model,
		UsedURL:    m.fallback.URL,
	}, nil
}

// HealthStatus returns the current health state of all known providers.
// Used by the /health endpoint and dashboard.
func (m *Manager) HealthStatus(ctx context.Context, providers []string) map[string]string {
	status := make(map[string]string, len(providers))
	for _, p := range providers {
		degradedKey := fmt.Sprintf("ajah:fallback:degraded:%s", p)
		exists, _ := m.rdb.Exists(ctx, degradedKey).Result()
		if exists > 0 {
			// Get remaining TTL
			ttl, _ := m.rdb.TTL(ctx, degradedKey).Result()
			status[p] = fmt.Sprintf("degraded (cooldown: %ds)", int(ttl.Seconds()))
		} else {
			failKey := fmt.Sprintf("ajah:fallback:failures:%s", p)
			count, _ := m.rdb.Get(ctx, failKey).Int()
			if count > 0 {
				status[p] = fmt.Sprintf("healthy (failures: %d/%d)", count, failureThreshold)
			} else {
				status[p] = "healthy"
			}
		}
	}
	return status
}

// rewriteModel replaces the "model" field value in a JSON body.
// It does a targeted string replacement rather than full JSON parse/re-encode
// to preserve the exact original formatting and avoid any field reordering.
func rewriteModel(body []byte, newModel string) ([]byte, error) {
	s := string(body)
	// Find "model": "..." and replace the value
	start := strings.Index(s, `"model"`)
	if start == -1 {
		return body, nil // no model field — return unchanged
	}
	colon := strings.Index(s[start:], `:`)
	if colon == -1 {
		return body, nil
	}
	afterColon := strings.TrimLeft(s[start+colon+1:], " \t\n\r")
	if !strings.HasPrefix(afterColon, `"`) {
		return body, nil
	}
	valStart := start + colon + 1 + strings.Index(s[start+colon+1:], `"`)
	valEnd := valStart + 1 + strings.Index(s[valStart+1:], `"`) + 1
	result := s[:valStart] + `"` + newModel + `"` + s[valEnd:]
	return []byte(result), nil
}
