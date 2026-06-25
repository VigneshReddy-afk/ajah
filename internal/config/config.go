package config

import (
	"fmt"
	"os"
	"strconv"
)

const (
	defaultPort                   = "8080"
	defaultRedisURL               = "redis://localhost:6379"
	defaultDatabaseURL            = "postgres://localhost:5432/observatory"
	defaultClickHouseURL          = "clickhouse://localhost:9000/default"
	defaultScorerURL              = "http://localhost:8001"
	defaultLogLevel               = "info"
	defaultMaxRequestBodyBytes    = int64(10 * 1024 * 1024)
	defaultAsyncWorkerCount       = 10
	defaultProviderTimeoutSeconds = 30
	defaultSessionTimeout         = 300
	defaultWebhookTimeoutSeconds  = 5
	defaultScorerTimeoutSeconds   = 30
)

// Config holds all runtime configuration for the ajah gateway.
type Config struct {
	Port                   string
	RedisURL               string
	DatabaseURL            string
	ClickHouseURL          string
	ScorerURL              string
	LogLevel               string
	MaxRequestBodyBytes    int64
	AsyncWorkerCount       int
	ProviderTimeoutSeconds int
	SessionTimeout         int
	WebhookTimeoutSeconds  int
	ScorerTimeoutSeconds   int
	OTelEndpoint           string
	CacheEnabled           bool
	CacheTTLSeconds        int
	// Self-healing fallback provider — optional.
	// If set, the gateway retries failed primary requests against this provider.
	FallbackModel        string // FALLBACK_MODEL e.g. "gpt-4o-mini"
	FallbackProviderURL  string // FALLBACK_PROVIDER_URL e.g. "https://api.openai.com/v1"
	FallbackAPIKey       string // FALLBACK_API_KEY
	SecurityBlockEnabled bool   // SECURITY_BLOCK_ENABLED
	// AuthEnabled controls whether login/auth is enforced.
	// Set to false (default) for self-hosted open-source use — no login required.
	// Set to true for managed cloud deployments with multiple tenants.
	AuthEnabled bool // AUTH_ENABLED (default: false)
}

// Load reads configuration from environment variables, falling back to defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                   envOrDefault("PORT", defaultPort),
		RedisURL:               envOrDefault("REDIS_URL", defaultRedisURL),
		DatabaseURL:            envOrDefault("DATABASE_URL", defaultDatabaseURL),
		ClickHouseURL:          envOrDefault("CLICKHOUSE_URL", defaultClickHouseURL),
		ScorerURL:              envOrDefault("SCORER_URL", defaultScorerURL),
		LogLevel:               envOrDefault("LOG_LEVEL", defaultLogLevel),
		OTelEndpoint:           envOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		MaxRequestBodyBytes:    defaultMaxRequestBodyBytes,
		AsyncWorkerCount:       defaultAsyncWorkerCount,
		ProviderTimeoutSeconds: defaultProviderTimeoutSeconds,
		SessionTimeout:         defaultSessionTimeout,
		WebhookTimeoutSeconds:  defaultWebhookTimeoutSeconds,
		ScorerTimeoutSeconds:   defaultScorerTimeoutSeconds,
	}

	if raw := os.Getenv("MAX_REQUEST_BODY_BYTES"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_REQUEST_BODY_BYTES %q: %w", raw, err)
		}
		cfg.MaxRequestBodyBytes = v
	}

	if raw := os.Getenv("ASYNC_WORKER_COUNT"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid ASYNC_WORKER_COUNT %q: %w", raw, err)
		}
		cfg.AsyncWorkerCount = v
	}

	if raw := os.Getenv("PROVIDER_TIMEOUT_SECONDS"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid PROVIDER_TIMEOUT_SECONDS %q: %w", raw, err)
		}
		cfg.ProviderTimeoutSeconds = v
	}

	if raw := os.Getenv("SESSION_TIMEOUT"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid SESSION_TIMEOUT %q: %w", raw, err)
		}
		cfg.SessionTimeout = v
	}

	if raw := os.Getenv("WEBHOOK_TIMEOUT_SECONDS"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid WEBHOOK_TIMEOUT_SECONDS %q: %w", raw, err)
		}
		cfg.WebhookTimeoutSeconds = v
	}

	if raw := os.Getenv("SCORER_TIMEOUT_SECONDS"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid SCORER_TIMEOUT_SECONDS %q: %w", raw, err)
		}
		cfg.ScorerTimeoutSeconds = v
	}

	cfg.CacheEnabled = os.Getenv("CACHE_ENABLED") == "true"

	if raw := os.Getenv("CACHE_TTL_SECONDS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			cfg.CacheTTLSeconds = v
		}
	}
	if cfg.CacheTTLSeconds == 0 {
		cfg.CacheTTLSeconds = 3600
	}

	// Fallback provider — all three must be set to enable self-healing
	cfg.FallbackModel = os.Getenv("FALLBACK_MODEL")
	cfg.FallbackProviderURL = os.Getenv("FALLBACK_PROVIDER_URL")
	cfg.FallbackAPIKey = os.Getenv("FALLBACK_API_KEY")
	cfg.SecurityBlockEnabled = os.Getenv("SECURITY_BLOCK_ENABLED") == "true"
	// AUTH_ENABLED defaults to false — self-hosted users get zero-friction access.
	// Set AUTH_ENABLED=true for managed cloud deployments.
	cfg.AuthEnabled = os.Getenv("AUTH_ENABLED") == "true"

	return cfg, nil
}

// Validate returns an error if required fields are empty or have invalid values.
func (c *Config) Validate() error {
	if c.Port == "" {
		return fmt.Errorf("Port is required")
	}
	if c.RedisURL == "" {
		return fmt.Errorf("RedisURL is required")
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("DatabaseURL is required")
	}
	if c.LogLevel == "" {
		return fmt.Errorf("LogLevel is required")
	}
	if c.MaxRequestBodyBytes <= 0 {
		return fmt.Errorf("MaxRequestBodyBytes must be greater than zero")
	}
	if c.AsyncWorkerCount <= 0 {
		return fmt.Errorf("AsyncWorkerCount must be greater than zero")
	}
	if c.ProviderTimeoutSeconds <= 0 {
		return fmt.Errorf("ProviderTimeoutSeconds must be greater than zero")
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
