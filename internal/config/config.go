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
	defaultLogLevel               = "info"
	defaultMaxRequestBodyBytes    = int64(10 * 1024 * 1024)
	defaultAsyncWorkerCount       = 10
	defaultProviderTimeoutSeconds = 30
)

// Config holds all runtime configuration for the ajah gateway.
type Config struct {
	Port                   string
	RedisURL               string
	DatabaseURL            string
	LogLevel               string
	MaxRequestBodyBytes    int64
	AsyncWorkerCount       int
	ProviderTimeoutSeconds int
}

// Load reads configuration from environment variables, falling back to defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                   envOrDefault("PORT", defaultPort),
		RedisURL:               envOrDefault("REDIS_URL", defaultRedisURL),
		DatabaseURL:            envOrDefault("DATABASE_URL", defaultDatabaseURL),
		LogLevel:               envOrDefault("LOG_LEVEL", defaultLogLevel),
		MaxRequestBodyBytes:    defaultMaxRequestBodyBytes,
		AsyncWorkerCount:       defaultAsyncWorkerCount,
		ProviderTimeoutSeconds: defaultProviderTimeoutSeconds,
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
